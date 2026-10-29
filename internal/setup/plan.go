// Package setup installs the whole stack in one guided run: the NAS shares,
// Proxmox backups on the NAS, and the chosen apps in the right order, with the
// apps connected to each other.
package setup

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"iglo/internal/agent"
	"iglo/internal/vpn"
)

// ErrInvalid is a plan that can't run, with what to fix.
var ErrInvalid = errors.New("invalid setup")

// Apps are the apps that the setup can install, in the order it installs them.
var Apps = []string{agent.JellyfinApp, agent.MediaStackApp, agent.PhotosApp, agent.MusicApp, agent.InboxApp, agent.GamingApp}

// shareRoles are the shares each app needs.
var shareRoles = map[string][]string{
	agent.JellyfinApp:   {"videos"},
	agent.MediaStackApp: {"videos"},
	agent.PhotosApp:     {"photos", "backups"},
	agent.MusicApp:      {"music"},
	agent.InboxApp:      {"inbox", "videos"},
	agent.GamingApp:     {"games"},
}

// readOnlyRoles are shares that the apps only read.
var readOnlyRoles = map[string]bool{"games": true, "inbox": true}

// memoryMiB are the memory limits the apps install with. They are limits,
// not reservations, and Gaming only runs while you play.
var memoryMiB = map[string]int{
	agent.JellyfinApp: 3072, agent.MediaStackApp: 4096, agent.PhotosApp: 8192,
	agent.MusicApp: 1024, agent.InboxApp: 2048, agent.GamingApp: 6144,
}

// The local disks that the setup's apps install with, in GiB. Jellyfin's and
// the media stack's container disks are fixed in their installers
// (deploy/stacks/jellyfin/install.sh and deploy/stacks/arr/install.sh); the
// media stack also gets a disk for its downloads.
const (
	jellyfinDiskGiB  = 16
	mediaDiskGiB     = 16
	downloadsDiskGiB = 200
)

// diskGiB is the local disk an app of the setup takes, for the disk budget.
func diskGiB(app string) int {
	switch app {
	case agent.JellyfinApp:
		return jellyfinDiskGiB
	case agent.MediaStackApp:
		return mediaDiskGiB + downloadsDiskGiB
	}
	return Plan{}.scopedAnswers(app, nil).DiskGB
}

// Plan is what the setup page asks. Its secrets stay in memory, never on disk.
type Plan struct {
	Apps  []string `json:"apps" yaml:"apps"`
	NAS   NAS      `json:"nas" yaml:"nas"`
	Admin Admin    `json:"admin" yaml:"admin"`
	VPN   VPN      `json:"vpn" yaml:"vpn"`
	// FlareSolverr and AnimeIndexers are the switches of the media stack.
	FlareSolverr  bool `json:"flareSolverr" yaml:"flareSolverr"`
	AnimeIndexers bool `json:"animeIndexers" yaml:"animeIndexers"`
	// AllowRestrictedFaceModels explicitly opts into InsightFace model weights
	// whose terms are more restrictive than the application source license.
	AllowRestrictedFaceModels bool `json:"allowRestrictedFaceModels" yaml:"allowRestrictedFaceModels"`
	// ProxmoxBackups keeps the backups of Proxmox on the Backups share.
	ProxmoxBackups bool `json:"proxmoxBackups" yaml:"proxmoxBackups"`
	// ConfirmPrivileged allows the privileged container of Gaming. A plan
	// file never has it: the confirmation is given again every time.
	ConfirmPrivileged bool `json:"confirmPrivileged" yaml:"-"`
	// Timezone is the time zone of the browser, for the processing window.
	Timezone string `json:"timezone" yaml:"timezone"`
	// Storage is the Proxmox storage of the container disks.
	Storage string `json:"storage" yaml:"storage"`
}

// NAS is where the shares are. By default each share has an account of its
// own, so an app can only reach its own folders. OneAccount uses Account for all.
type NAS struct {
	Server     string           `json:"server" yaml:"server"`
	OneAccount bool             `json:"oneAccount" yaml:"oneAccount"`
	Account    Account          `json:"account" yaml:"account"`
	Shares     map[string]Share `json:"shares" yaml:"shares"`
}

type Account struct {
	Username string `json:"username" yaml:"username"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
}

// Share is one shared folder of the NAS, like Videos.
type Share struct {
	Share   string `json:"share" yaml:"share"`
	Account `yaml:",inline"`
}

// Admin is the one login for Jellyfin, the media stack, Seerr and Immich.
type Admin struct {
	Username string `json:"username" yaml:"username"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	// Email is the login of Immich, which needs an address.
	Email string `json:"email" yaml:"email"`
}

// VPN is the VPN of the media stack. The secrets and the PIA username stay
// out of the plan file.
type VPN struct {
	// Provider is protonvpn or pia. Empty is ProtonVPN, as in plans from before PIA.
	Provider string `json:"provider" yaml:"provider,omitempty"`
	// Locations are countries (ProtonVPN) or regions (PIA), separated by
	// commas, or "Dedicated IP" for a PIA dedicated IP.
	Locations string `json:"locations" yaml:"locations,omitempty"`
	// Countries is the old name of Locations, read from plan files from before PIA.
	Countries         string `json:"-" yaml:"countries,omitempty"`
	WireGuardKey      string `json:"wireguardKey,omitempty" yaml:"wireguardKey,omitempty"`
	PIAUsername       string `json:"piaUsername,omitempty" yaml:"piaUsername,omitempty"`
	PIAPassword       string `json:"piaPassword,omitempty" yaml:"piaPassword,omitempty"`
	PIADedicatedToken string `json:"piaDedicatedToken,omitempty" yaml:"piaDedicatedToken,omitempty"`
}

// settings are the answers of the media stack for this VPN.
func (v VPN) settings() agent.VPNSettings {
	provider, locations := v.Provider, strings.TrimSpace(v.Locations)
	if provider == "" {
		provider = vpn.ProtonVPN
	}
	if locations == "" {
		locations = strings.TrimSpace(v.Countries)
	}
	return agent.VPNSettings{
		Provider: provider, Locations: locations, WireGuardPrivateKey: v.WireGuardKey,
		PIAUsername: strings.TrimSpace(v.PIAUsername), PIAPassword: v.PIAPassword, PIADedicatedToken: v.PIADedicatedToken,
	}
}

var (
	adminPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

func (p Plan) wants(app string) bool { return slices.Contains(p.Apps, app) }

// roles are the shares that the plan needs, in a fixed order.
func (p Plan) roles() []string {
	roles := []string{}
	for _, role := range []string{"videos", "photos", "backups", "music", "inbox", "games"} {
		needed := role == "backups" && p.ProxmoxBackups
		for _, app := range p.Apps {
			needed = needed || slices.Contains(shareRoles[app], role)
		}
		if needed {
			roles = append(roles, role)
		}
	}
	return roles
}

// account is the NAS login of a share.
func (p Plan) account(role string) Account {
	if p.NAS.OneAccount {
		return p.NAS.Account
	}
	return p.NAS.Shares[role].Account
}

// bindingInput is the share of role, with its ID on the Storage page.
func (p Plan) bindingInput(id, role string) agent.BindingInput {
	account := p.account(role)
	return agent.BindingInput{
		ID: id, Role: role, Server: strings.TrimSpace(p.NAS.Server), Share: strings.TrimSpace(p.NAS.Shares[role].Share),
		Username: account.Username, Password: account.Password, ReadOnly: readOnlyRoles[role],
	}
}

func (p Plan) jellyfinAnswers(videos string) agent.JellyfinAnswers {
	return agent.JellyfinAnswers{
		VideosBinding: videos, MoviesFolder: "movies", SeriesFolder: "series",
		AnimeFolder: "anime", AnimeMoviesFolder: "anime-movies",
		AdminUsername: p.Admin.Username, AdminPassword: p.Admin.Password, Storage: p.Storage,
	}
}

func (p Plan) mediaAnswers(videos, jellyfinKey string, withJellyfin bool) agent.MediaStackAnswers {
	answers := agent.MediaStackAnswers{
		VideosBinding: videos, MoviesFolder: "movies", SeriesFolder: "series",
		AnimeFolder: "anime", AnimeMoviesFolder: "anime-movies",
		VPN: p.VPN.settings(), SubtitleLanguages: "en",
		Username: p.Admin.Username, Password: p.Admin.Password, RestartJellyfin: true, Storage: p.Storage,
		DownloadsSize: downloadsDiskGiB, FlareSolverr: p.FlareSolverr, AnimeIndexers: p.AnimeIndexers,
	}
	// The media stack sets up Seerr with the admin of Jellyfin, and adds the
	// libraries with the key that the Jellyfin installer made, when it has it.
	if withJellyfin {
		answers.JellyfinAPIKey = jellyfinKey
		answers.JellyfinAdminUsername, answers.JellyfinAdminPassword = p.Admin.Username, p.Admin.Password
	}
	return answers
}

// scopedAnswers are the answers of Photos, Music, Inbox or Gaming, with the
// defaults of their install forms.
func (p Plan) scopedAnswers(app string, bindings map[string]string) agent.ScopedAnswers {
	answers := agent.ScopedAnswers{Storage: p.Storage, Cores: 2, MemoryMiB: memoryMiB[app], DiskGB: 64}
	switch app {
	case agent.PhotosApp:
		answers.LibraryBinding, answers.BackupBinding, answers.Cores = bindings["photos"], bindings["backups"], 4
	case agent.MusicApp:
		answers.LibraryBinding = bindings["music"]
	case agent.InboxApp:
		answers.LibraryBinding, answers.OutputBinding = bindings["inbox"], bindings["videos"]
	case agent.GamingApp:
		answers.LibraryBinding, answers.Cores, answers.DiskGB, answers.Privileged = bindings["games"], 4, 96, true
	}
	return answers
}

// Validate checks the whole plan before anything happens, with the same
// checks as each installer, so a typo can't stop the run halfway. Shares of
// a covered role are connected already, so they need no answers.
func (p Plan) Validate(covered map[string]bool) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	if len(p.Apps) == 0 {
		return invalid("choose at least one app")
	}
	for _, app := range p.Apps {
		if !slices.Contains(Apps, app) {
			return invalid("unknown app %q", app)
		}
	}
	if !adminPattern.MatchString(p.Admin.Username) {
		return invalid("the admin username can have letters, digits, dots, dashes and underscores")
	}
	if len([]rune(p.Admin.Password)) < 12 || len(p.Admin.Password) > 128 || strings.ContainsFunc(p.Admin.Password, unicode.IsControl) {
		return invalid("the admin password must be 12 to 128 characters")
	}
	if p.wants(agent.PhotosApp) && !emailPattern.MatchString(p.Admin.Email) {
		return invalid("Photos needs an email address for its admin")
	}
	if p.wants(agent.GamingApp) && !p.ConfirmPrivileged {
		return invalid("confirm the privileged container of Gaming")
	}
	if _, err := time.LoadLocation(p.Timezone); p.wants(agent.PhotosApp) && (p.Timezone == "" || err != nil) {
		return invalid("unknown time zone %q", p.Timezone)
	}
	bindings := map[string]string{}
	for _, role := range p.roles() {
		bindings[role] = role
		if covered[role] {
			continue
		}
		if err := p.bindingInput(role, role).Validate(); err != nil {
			return invalid("the %s share: give the NAS address, the share name and its account", role)
		}
	}
	videos := bindings["videos"]
	if p.wants(agent.JellyfinApp) {
		if err := p.jellyfinAnswers(videos).Validate(); err != nil {
			return invalid("Jellyfin: %s", strings.TrimPrefix(err.Error(), agent.ErrInvalidAnswers.Error()+": "))
		}
	}
	if p.wants(agent.MediaStackApp) {
		if err := p.mediaAnswers(videos, "", false).Validate(); err != nil {
			return invalid("media stack: %s", strings.TrimPrefix(err.Error(), agent.ErrInvalidAnswers.Error()+": "))
		}
	}
	for _, app := range []string{agent.PhotosApp, agent.MusicApp, agent.InboxApp, agent.GamingApp} {
		if p.wants(app) {
			if err := p.scopedAnswers(app, bindings).ValidateFor(app); err != nil {
				return invalid("%s: %s", app, strings.TrimPrefix(err.Error(), agent.ErrInvalidAnswers.Error()+": "))
			}
		}
	}
	return nil
}
