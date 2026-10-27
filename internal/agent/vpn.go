package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"iglo/internal/vpn"
)

var (
	piaUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)
	// A PIA dedicated IP token is 32 characters and starts with DIP, as PIA's
	// manual-connections scripts check.
	piaTokenPattern = regexp.MustCompile(`^DIP[A-Za-z0-9]{29}$`)
)

// VPNSettings are the VPN of the media stack: ProtonVPN with a WireGuard key,
// or PIA with its login and an optional dedicated IP token. Locations are
// countries (ProtonVPN) or regions (PIA), separated by commas. When changing
// the VPN, an empty secret keeps the one in the container.
type VPNSettings struct {
	Provider            string `json:"provider"`
	Locations           string `json:"locations"`
	WireGuardPrivateKey string `json:"wireguardPrivateKey"`
	PIAUsername         string `json:"piaUsername"`
	PIAPassword         string `json:"piaPassword"`
	PIADedicatedToken   string `json:"piaDedicatedToken"`
}

// Validate checks a change: the secrets may be empty to keep the old ones.
func (v VPNSettings) Validate() error {
	return v.validate(false)
}

// validate checks the settings. Only the secrets of the chosen provider
// count: the form may still hold another provider's. With secrets, the ones
// the provider needs must be there, as for a new install. The secrets go on
// stdin and are sealed on the host, never in a file in the container, so any
// character but a line break is fine.
func (v VPNSettings) validate(secrets bool) error {
	invalid := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalidAnswers, message) }
	v = v.forProvider()
	dedicated := v.Locations == vpn.Dedicated
	oneLine := func(secret string) bool {
		return len(secret) <= 256 && !strings.ContainsFunc(secret, unicode.IsControl)
	}

	switch {
	case !vpn.ValidProvider(v.Provider):
		return invalid("choose ProtonVPN or PIA")
	case !vpn.ValidLocations(v.Provider, v.locations()):
		return invalid("choose 1 to 10 VPN locations from the list, or only the dedicated IP")
	case v.WireGuardPrivateKey != "" && !validWireGuardKey(v.WireGuardPrivateKey):
		return invalid("the WireGuard private key must be 44 characters of base64")
	case v.PIAUsername != "" && !piaUsernamePattern.MatchString(v.PIAUsername):
		return invalid("the PIA username is like p1234567")
	case !oneLine(v.PIAPassword):
		return invalid("the PIA password must be at most 256 characters, without line breaks")
	case v.PIADedicatedToken != "" && !piaTokenPattern.MatchString(v.PIADedicatedToken):
		return invalid("a PIA dedicated IP token is 32 characters and starts with DIP")
	case !secrets:
		return nil
	case v.Provider == vpn.ProtonVPN && v.WireGuardPrivateKey == "":
		return invalid("ProtonVPN needs a WireGuard private key")
	case v.Provider == vpn.PIA && (v.PIAUsername == "" || v.PIAPassword == ""):
		return invalid("PIA needs its username and password")
	case dedicated && v.PIADedicatedToken == "":
		return invalid("the dedicated IP needs its token")
	}

	return nil
}

// forProvider leaves out the secrets of the other provider. The host keeps
// the ones it has, so switching back needs no new login.
func (v VPNSettings) forProvider() VPNSettings {
	if v.Provider == vpn.PIA {
		v.WireGuardPrivateKey = ""
	} else {
		v.PIAUsername, v.PIAPassword, v.PIADedicatedToken = "", "", ""
	}
	return v
}

// missingSecrets says whether the settings lack a secret that the provider needs.
func (v VPNSettings) missingSecrets() bool {
	return v.validate(true) != nil && v.validate(false) == nil
}

func (v VPNSettings) locations() []string {
	locations := strings.Split(v.Locations, ",")
	for index, location := range locations {
		locations[index] = strings.TrimSpace(location)
	}
	return locations
}

// lines are the VPN answers for the installer, with only the secrets of
// the chosen provider.
func (v VPNSettings) lines() []string {
	v = v.forProvider()
	return []string{
		"VPN_PROVIDER=" + v.Provider,
		"VPN_LOCATIONS=" + strings.Join(v.locations(), ","),
		"WIREGUARD_PRIVATE_KEY=" + v.WireGuardPrivateKey,
		"PIA_USERNAME=" + v.PIAUsername,
		"PIA_PASSWORD=" + v.PIAPassword,
		"PIA_DEDICATED_TOKEN=" + v.PIADedicatedToken,
	}
}

// withSecretsFrom fills the empty secrets from saved, for a retry.
func (v VPNSettings) withSecretsFrom(saved VPNSettings) VPNSettings {
	if v.WireGuardPrivateKey == "" {
		v.WireGuardPrivateKey = saved.WireGuardPrivateKey
	}
	if v.PIAUsername == "" {
		v.PIAUsername = saved.PIAUsername
	}
	if v.PIAPassword == "" {
		v.PIAPassword = saved.PIAPassword
	}
	if v.PIADedicatedToken == "" {
		v.PIADedicatedToken = saved.PIADedicatedToken
	}
	return v
}

// withoutSecrets is what the browser and the retry file may see. The PIA
// username is a login too, so it goes as well.
func (v VPNSettings) withoutSecrets() VPNSettings {
	v.WireGuardPrivateKey, v.PIAUsername, v.PIAPassword, v.PIADedicatedToken = "", "", "", ""
	return v
}

// VPNView is the VPN of an installed media stack, without its secrets.
type VPNView struct {
	Provider  string `json:"provider"`
	Locations string `json:"locations"`
	// The Has fields say which secrets the host keeps for the stack, so a
	// form can leave them empty to keep them.
	HasWireGuardKey   bool `json:"hasWireguardKey"`
	HasPIALogin       bool `json:"hasPiaLogin"`
	HasDedicatedToken bool `json:"hasDedicatedToken"`
}

// vpnViewScript reads the VPN lines of .env, which has no secrets, except the
// WireGuard key of a stack from before the sealed secrets. That one comes out
// as "set", so it never leaves the container.
var vpnViewScript = []string{"sed", "-n", "-E",
	"-e", `s/^(VPN_PROVIDER|VPN_COUNTRIES|VPN_REGIONS|GLUETUN_PROVIDER)=(.*)$/\1=\2/p`,
	"-e", `s/^(WIREGUARD_PRIVATE_KEY)=.+$/\1=set/p`,
	"/opt/arr/.env"}

// VPN reads the VPN settings of the media stack in container vmid. Which
// secrets it has comes from the sealed files on the host (deploy/secrets.sh).
func (i *AppInstaller) VPN(ctx context.Context, vmid int) (VPNView, error) {
	if vmid < 100 || vmid > 999999999 {
		return VPNView{}, ErrInvalidContainer
	}

	args := append([]string{"exec", strconv.Itoa(vmid), "--"}, vpnViewScript...)
	output, err := i.output(ctx, "pct", args...)
	if err != nil {
		return VPNView{}, fmt.Errorf("could not read the VPN settings of container %d: %w", vmid, err)
	}

	view := parseVPNView(string(output))
	sealed := func(name string) bool {
		_, err := os.Stat(filepath.Join(i.credentialsDir(), strconv.Itoa(vmid), "keep", name+".cred"))
		return err == nil
	}
	view.HasWireGuardKey = view.HasWireGuardKey || sealed("wireguard_private_key")
	view.HasPIALogin = sealed("pia_username") && sealed("pia_password")
	view.HasDedicatedToken = sealed("pia_dedicated_token")

	return view, nil
}

func (i *AppInstaller) credentialsDir() string {
	if i.CredentialsDir == "" {
		return "/etc/iglo/credentials"
	}
	return i.CredentialsDir
}

// parseVPNView reads the output of vpnViewScript. A stack from before the
// provider choice has only ProtonVPN countries, maybe in free text: the ones
// that aren't on the list fall out, so the form can choose them again.
func parseVPNView(output string) VPNView {
	env := map[string]string{}
	for line := range strings.Lines(output) {
		if key, value, found := strings.Cut(strings.TrimSpace(line), "="); found {
			if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
				value = strings.ReplaceAll(value[1:len(value)-1], `\'`, "'")
			}
			env[key] = value
		}
	}

	view := VPNView{Provider: env["VPN_PROVIDER"], HasWireGuardKey: env["WIREGUARD_PRIVATE_KEY"] == "set"}
	locations := env["VPN_COUNTRIES"]
	switch {
	case view.Provider == vpn.PIA && env["GLUETUN_PROVIDER"] == "custom":
		locations = vpn.Dedicated
	case view.Provider == vpn.PIA:
		locations = env["VPN_REGIONS"]
	default:
		view.Provider = vpn.ProtonVPN
	}

	var known []string
	for _, location := range strings.Split(locations, ",") {
		location = strings.TrimSpace(location)
		if location != "" && vpn.ValidLocations(view.Provider, []string{location}) && !slices.Contains(known, location) {
			known = append(known, location)
		}
	}
	view.Locations = strings.Join(known, ",")

	return view
}
