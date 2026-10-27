package arr

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"iglo/internal/vpn"
)

// VPN is a choice of VPN for the media stack. The host passes the secrets
// on stdin, also the ones it keeps; an empty one falls back to a secret in
// .env, where a stack from before the sealed secrets has its WireGuard key.
type VPN struct {
	Provider  string
	Locations []string
	// WireGuardKey is the private key of ProtonVPN.
	WireGuardKey string
	// PIAUsername and PIAPassword are the PIA login, also for port forwarding.
	PIAUsername string
	PIAPassword string
	// PIADedicatedToken is a PIA dedicated IP token, for the location vpn.Dedicated.
	PIADedicatedToken string
}

// vpnKeys are the lines of .env that ApplyVPN owns. compose.yaml hands the
// GLUETUN_ ones to Gluetun; the others say what was chosen.
var vpnKeys = []string{
	"VPN_PROVIDER", "VPN_COUNTRIES", "VPN_REGIONS",
	"GLUETUN_PROVIDER", "GLUETUN_VPN_TYPE", "GLUETUN_OPENVPN_CONFIG", "GLUETUN_SERVER_NAMES",
	"GLUETUN_PORT_FORWARD_ONLY", "GLUETUN_PORT_FORWARDING_PROVIDER",
}

// secretKeys are lines with secrets that .env of an older stack may have.
// ApplyVPN takes them out: the secrets now live sealed on the host.
var secretKeys = []string{
	"WIREGUARD_PRIVATE_KEY", "PIA_USERNAME", "PIA_PASSWORD", "PIA_DEDICATED_TOKEN",
	"GLUETUN_OPENVPN_USER", "GLUETUN_OPENVPN_PASSWORD",
}

// SecretFiles are the secret files that Gluetun reads from /run/secrets,
// named as it looks for them: the setting in lower case (v3.41.3,
// internal/configuration/sources/secrets). Gluetun prefers them to
// environment variables.
var SecretFiles = []string{
	"wireguard_private_key", "openvpn_user", "openvpn_password",
	"vpn_port_forwarding_username", "vpn_port_forwarding_password",
}

// dedicatedConfig is the OpenVPN file of a PIA dedicated IP, in the gluetun
// folder of the stack, which Gluetun sees as /gluetun.
const dedicatedConfig = "pia-dedicated.ovpn"

// ApplyVPN writes the VPN choice in the .env at envPath, and for a dedicated
// IP the OpenVPN file in gluetunDir. It returns the secret files for Gluetun,
// each name of SecretFiles with its value or empty, for the host to seal.
// .env keeps no secret. A stack from before the provider choice has only
// VPN_COUNTRIES and WIREGUARD_PRIVATE_KEY, which means ProtonVPN.
func ApplyVPN(ctx context.Context, choice VPN, envPath, gluetunDir string, pia *PIAClient) (map[string]string, error) {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return nil, err
	}
	old := parseEnv(string(data))
	keep := func(value, key string) string {
		if value == "" {
			return old[key]
		}
		return value
	}
	choice.WireGuardKey = keep(choice.WireGuardKey, "WIREGUARD_PRIVATE_KEY")
	choice.PIAUsername = keep(choice.PIAUsername, "PIA_USERNAME")
	choice.PIAPassword = keep(choice.PIAPassword, "PIA_PASSWORD")
	choice.PIADedicatedToken = keep(choice.PIADedicatedToken, "PIA_DEDICATED_TOKEN")
	for index, location := range choice.Locations {
		choice.Locations[index] = strings.TrimSpace(location)
	}

	values, secrets, err := gluetunValues(ctx, choice, gluetunDir, pia)
	if err != nil {
		return nil, err
	}

	var lines []string
	for line := range strings.Lines(string(data)) {
		key, _, _ := strings.Cut(strings.TrimSpace(line), "=")
		if !slices.Contains(vpnKeys, key) && !slices.Contains(secretKeys, key) {
			lines = append(lines, strings.TrimRight(line, "\n"))
		}
	}
	for _, key := range vpnKeys {
		if value := values[key]; value != "" {
			lines = append(lines, key+"="+quoteEnv(value))
		}
	}

	if err := writeFileAtomic(envPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return nil, err
	}
	return secrets, nil
}

// gluetunValues are the .env values and the secret files of a choice, after the checks.
func gluetunValues(ctx context.Context, choice VPN, gluetunDir string, pia *PIAClient) (map[string]string, map[string]string, error) {
	if !vpn.ValidProvider(choice.Provider) {
		return nil, nil, fmt.Errorf("unknown VPN provider %q", choice.Provider)
	}
	if !vpn.ValidLocations(choice.Provider, choice.Locations) {
		return nil, nil, fmt.Errorf("the VPN locations %q are not locations of %s with port forwarding", choice.Locations, choice.Provider)
	}
	for _, secret := range []string{choice.WireGuardKey, choice.PIAUsername, choice.PIAPassword, choice.PIADedicatedToken} {
		if len(secret) > 256 || strings.ContainsFunc(secret, unicode.IsControl) {
			return nil, nil, errors.New("a VPN secret is longer than 256 characters or has a line break")
		}
	}
	locations := strings.Join(choice.Locations, ",")
	secrets := map[string]string{}
	for _, name := range SecretFiles {
		secrets[name] = ""
	}

	if choice.Provider == vpn.ProtonVPN {
		if choice.WireGuardKey == "" {
			return nil, nil, errors.New("ProtonVPN needs a WireGuard private key")
		}
		secrets["wireguard_private_key"] = choice.WireGuardKey
		return map[string]string{
			"VPN_PROVIDER": vpn.ProtonVPN, "VPN_COUNTRIES": locations,
			"GLUETUN_PROVIDER": "protonvpn", "GLUETUN_VPN_TYPE": "wireguard", "GLUETUN_PORT_FORWARD_ONLY": "on",
		}, secrets, nil
	}

	if choice.PIAUsername == "" || choice.PIAPassword == "" {
		return nil, nil, errors.New("PIA needs its username and password")
	}
	values := map[string]string{"VPN_PROVIDER": vpn.PIA, "GLUETUN_VPN_TYPE": "openvpn"}
	// PIA's port forwarding logs in with the account, also for a dedicated IP.
	secrets["vpn_port_forwarding_username"] = choice.PIAUsername
	secrets["vpn_port_forwarding_password"] = choice.PIAPassword
	if locations != vpn.Dedicated {
		// Gluetun has PIA over OpenVPN only (v3.41.3), and finds the servers itself.
		values["VPN_REGIONS"] = locations
		values["GLUETUN_PROVIDER"] = "private internet access"
		values["GLUETUN_PORT_FORWARD_ONLY"] = "on"
		secrets["openvpn_user"] = choice.PIAUsername
		secrets["openvpn_password"] = choice.PIAPassword
		return values, secrets, nil
	}

	if choice.PIADedicatedToken == "" {
		return nil, nil, errors.New("a PIA dedicated IP needs its token")
	}
	server, err := pia.DedicatedIP(ctx, choice.PIAUsername, choice.PIAPassword, choice.PIADedicatedToken)
	if err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomic(filepath.Join(gluetunDir, dedicatedConfig), dedicatedOpenVPN(server), 0o600); err != nil {
		return nil, nil, err
	}
	// Gluetun's custom provider connects to the one server, and PIA's port
	// forwarding finds it by its name, with the normal PIA login. The VPN
	// login is the token with no password, as in PIA's own manual-connections
	// scripts. Checked against Gluetun v3.41.3 and pia-foss/manual-connections
	// 645377e on 27 October 2026.
	values["GLUETUN_PROVIDER"] = "custom"
	values["GLUETUN_OPENVPN_CONFIG"] = "/gluetun/" + dedicatedConfig
	values["GLUETUN_SERVER_NAMES"] = server.Name
	values["GLUETUN_PORT_FORWARD_ONLY"] = "off"
	values["GLUETUN_PORT_FORWARDING_PROVIDER"] = "private internet access"
	secrets["openvpn_user"] = "dedicated_ip_" + choice.PIADedicatedToken

	return values, secrets, nil
}

//go:embed pia-ca.pem
var piaCA string

// dedicatedOpenVPN is the OpenVPN file of a dedicated IP, with the settings
// of Gluetun's own PIA provider for the strong preset: UDP 8080, AES-256-GCM
// and PIA's strong CA. Gluetun adds the login, the protocol and the device.
func dedicatedOpenVPN(server DedicatedServer) []byte {
	return []byte("client\n" +
		"proto udp\n" +
		"remote " + server.IP + " 8080\n" +
		"nobind\n" +
		"remote-cert-tls server\n" +
		"reneg-sec 0\n" +
		"mssfix 1320\n" +
		"data-ciphers AES-256-GCM\n" +
		"<ca>\n" + piaCA + "</ca>\n")
}

// quoteEnv puts a value in single quotes when it needs them, like a country
// with a space or an apostrophe. Docker Compose reads single-quoted values
// literally, except \' for a quote (tested with Compose v5.1.2). .env holds
// no secrets, so no value ends in a backslash.
func quoteEnv(value string) string {
	if strings.ContainsAny(value, " $#'\"\\`") {
		return "'" + strings.ReplaceAll(value, "'", `\'`) + "'"
	}
	return value
}

// parseEnv reads KEY=value lines, and takes single quotes off.
func parseEnv(data string) map[string]string {
	env := map[string]string{}
	for line := range strings.Lines(data) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, found := strings.Cut(line, "="); found {
			if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
				value = strings.ReplaceAll(value[1:len(value)-1], `\'`, "'")
			}
			env[key] = value
		}
	}
	return env
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// DedicatedServer is the server of a PIA dedicated IP.
type DedicatedServer struct {
	IP   string
	Name string
}

// PIAClient asks PIA's API about a dedicated IP.
type PIAClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewPIAClient talks to PIA's API, as its manual-connections scripts do.
func NewPIAClient() *PIAClient {
	return &PIAClient{BaseURL: "https://www.privateinternetaccess.com/api/client/v2", HTTP: &http.Client{Timeout: 30 * time.Second}}
}

var errPIA = errors.New("PIA")

// DedicatedIP logs in to PIA and looks up the server of a dedicated IP token.
func (c *PIAClient) DedicatedIP(ctx context.Context, username, password, token string) (DedicatedServer, error) {
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	writer.WriteField("username", username)
	writer.WriteField("password", password)
	writer.Close()

	var login struct {
		Token string `json:"token"`
	}
	if err := c.post(ctx, "/token", writer.FormDataContentType(), "", &form, &login); err != nil {
		return DedicatedServer{}, fmt.Errorf("log in to PIA: %w", err)
	}
	if login.Token == "" {
		return DedicatedServer{}, fmt.Errorf("%w refused the username or password", errPIA)
	}

	body, _ := json.Marshal(map[string][]string{"tokens": {token}})
	var dedicated []struct {
		Status string `json:"status"`
		IP     string `json:"ip"`
		CN     string `json:"cn"`
		ID     string `json:"id"`
	}
	if err := c.post(ctx, "/dedicated_ip", "application/json", login.Token, bytes.NewReader(body), &dedicated); err != nil {
		return DedicatedServer{}, fmt.Errorf("look up the dedicated IP: %w", err)
	}
	switch {
	case len(dedicated) == 0 || dedicated[0].Status != "active":
		return DedicatedServer{}, fmt.Errorf("%w does not know this dedicated IP token, or it expired", errPIA)
	case strings.HasPrefix(dedicated[0].ID, "us_"):
		// PIA's get_dip.sh: US locations have no port forwarding.
		return DedicatedServer{}, fmt.Errorf("%w has no port forwarding for a dedicated IP in the US, and qBittorrent needs it", errPIA)
	case !validHost(dedicated[0].IP) || !validHost(dedicated[0].CN):
		return DedicatedServer{}, fmt.Errorf("%w sent an unexpected dedicated IP", errPIA)
	}

	return DedicatedServer{IP: dedicated[0].IP, Name: dedicated[0].CN}, nil
}

func (c *PIAClient) post(ctx context.Context, path, contentType, token string, body io.Reader, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", contentType)
	if token != "" {
		request.Header.Set("Authorization", "Token "+token)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w answered %s", errPIA, response.Status)
	}
	return json.NewDecoder(response.Body).Decode(out)
}

// validHost allows an IP address or a host name, and nothing that could
// change the OpenVPN file or .env.
func validHost(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
