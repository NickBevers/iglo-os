// Package apps is the app catalog: stacks that the host agent installs in
// their own container, from installers in the release bundle.
package apps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"iglo/internal/agent"
	"iglo/internal/events"
	"iglo/internal/proxmox"
	"iglo/internal/vpn"
)

var (
	ErrUnknownApp   = errors.New("unknown app")
	ErrInstalled    = errors.New("this app is already installed")
	ErrBusy         = errors.New("an app is already being installed, updated or removed")
	ErrNotInstalled = errors.New("this app is not installed")
	ErrNotManaged   = errors.New("Iglo-OS did not install this container, so it can't update or remove it. Update it on the Updates page")
	ErrNotRunning   = errors.New("the container of this app is not running, start it first")
	ErrPlanned      = errors.New("this app comes in a later version of Iglo-OS")
)

// snapshotPrefix is the prefix of the Updates page too, which keeps the newest 3 of these snapshots.
const snapshotPrefix = "iglo_"

var (
	// versionPattern is the line that the installers write in the description of an app container.
	versionPattern = regexp.MustCompile(`(?m)^iglo-version: (\S+)$`)
	// addressPattern is the public address of an app, like the playit.gg address of Minecraft.
	addressPattern = regexp.MustCompile(`(?m)^iglo-address: (\S+)$`)
)

type Agent interface {
	InstallApp(ctx context.Context, app string, request agent.InstallRequest) error
	RetryApp(ctx context.Context, app string) error
	SavedAppAnswers(ctx context.Context, app string) (*agent.SavedAnswers, error)
	ForgetAppAnswers(ctx context.Context, app string) error
	UpdateApp(ctx context.Context, app string, vmid int) error
	RemoveApp(ctx context.Context, app string, vmid int) error
	VPN(ctx context.Context, vmid int) (agent.VPNView, error)
	ChangeVPN(ctx context.Context, vmid int, settings agent.VPNSettings) error
	MinecraftPlayers(ctx context.Context, vmid int) (agent.MinecraftPlayers, error)
	ChangeMinecraftPlayers(ctx context.Context, vmid int, players agent.MinecraftPlayers) error
	AppStats(ctx context.Context, app string, vmid int) (agent.AppStats, error)
	AppInstallStatus(ctx context.Context) (agent.AppInstallStatus, error)
}

type Proxmox interface {
	Resources(ctx context.Context) ([]proxmox.Resource, error)
	ContainerStorages(ctx context.Context, node string) ([]proxmox.BackupStorage, error)
	ContainerInterfaces(ctx context.Context, node string, vmid int) ([]proxmox.Interface, error)
	ContainerDescription(ctx context.Context, node string, vmid int) (string, error)
	SnapshotSupported(ctx context.Context, node string, vmid int) (bool, error)
	CreateSnapshot(ctx context.Context, node string, vmid int, name, description string) error
	RollbackSnapshot(ctx context.Context, node string, vmid int, name string) error
}

type Link struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Port        int    `json:"-"`
	// Address is set for an app that is not a website, like a game server.
	// URL is then ip:port, to copy into the game.
	Address bool   `json:"address,omitempty"`
	URL     string `json:"url,omitempty"`
}

type App struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Tag marks the container of the app in Proxmox.
	Tag string `json:"-"`
	// MatchName is a container name that also counts as this app, for an
	// install that Iglo-OS did not make, like Jellyfin from community-scripts.
	MatchName string `json:"-"`
	// Subdomain is the name that Iglo-OS forwards to the app, like seerr in seerr.iglo.local.
	Subdomain string `json:"-"`
	// Stack is set when a Iglo-OS release can change the app, like the compose.yaml of the media stack.
	Stack bool `json:"-"`
	// Planned is set for an app that Iglo-OS can't install yet. It already
	// shows on the Apps page and the dashboard, so its place is clear.
	Planned bool `json:"planned"`
	// Stats is set when the host agent reports AppStats for the app.
	Stats bool `json:"-"`
	// HostStats is set when the host reads the stats itself, so they don't
	// need the container to run, like the game library on the Games share.
	HostStats bool   `json:"-"`
	Links     []Link `json:"links"`
}

var Catalog = []App{{
	ID:          agent.JellyfinApp,
	Name:        "Jellyfin",
	Description: "Watch your movies and series on every device. Transcodes with the GPU of the host, when it has one.",
	Tag:         "jellyfin",
	MatchName:   "jellyfin",
	Subdomain:   "jellyfin",
	Links:       []Link{{Name: "Jellyfin", Description: "Watch", Port: 8096}},
}, {
	ID:          agent.MediaStackApp,
	Name:        "Media stack",
	Description: "Request, find and download movies and series for Jellyfin. Downloads go through ProtonVPN or PIA.",
	Tag:         "media",
	Subdomain:   "seerr",
	Stack:       true,
	Links: []Link{
		{Name: "Seerr", Description: "Requests", Port: 5055},
		{Name: "Radarr", Description: "Movies", Port: 7878},
		{Name: "Sonarr", Description: "Series", Port: 8989},
		{Name: "Prowlarr", Description: "Indexers", Port: 9696},
		{Name: "Bazarr", Description: "Subtitles", Port: 6767},
		{Name: "qBittorrent", Description: "Downloads", Port: 8080},
	},
}, {
	ID:          "music",
	Name:        "Music",
	Description: "Downloads songs from YouTube Music with their cover art, for Jellyfin and Finamp. Music videos only when you ask.",
	Tag:         "music",
	Stack:       true,
	Stats:       true,
	Links:       []Link{},
}, {
	ID:          "photos",
	Name:        "Photos",
	Description: "Immich: the phones back up their photos to the NAS, with faces and search.",
	Tag:         "photos",
	Stack:       true,
	Links:       []Link{{Name: "Immich", Description: "Photos", Port: 2283}},
}, {
	ID:          "inbox",
	Name:        "Inbox",
	Description: "FFmpeg processes your own videos into the Home videos output library. Choose an original-codec or playback profile; sources stay read only.",
	Tag:         "inbox",
	Stack:       true,
	Links:       []Link{{Name: "FileFlows", Description: "Converter", Port: 19200}},
}, {
	ID:          agent.GamingApp,
	Name:        "Gaming",
	Description: "Experimental. Retro games from your Games share, streamed to your PC or TV with Moonlight. Wolf runs in a privileged container that only starts while you play.",
	Tag:         "gaming",
	Stack:       true,
	Stats:       true,
	HostStats:   true,
	Links:       []Link{},
}, {
	ID: agent.HeadscaleApp, Name: "Headscale", Description: "Your own private network controller and relay. Native services in a 512 MiB container. Requires public HTTPS and a static LAN address.", Tag: "headscale", Stack: true, Links: []Link{},
}}

type AppView struct {
	ControllerHost string `json:"controllerHost,omitempty"`
	App
	Installed bool `json:"installed"`
	// Managed is set when Iglo-OS installed the container, so it can update and remove it.
	Managed bool `json:"managed"`
	// Version is the Iglo-OS release of the stack in the container. Empty for a
	// stack from before versions were saved.
	Version         string `json:"version,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	// HostURL is the address of the app through Iglo-OS, like https://seerr.iglo.local.
	HostURL string `json:"hostUrl,omitempty"`
	// PublicAddress is where people outside your network reach the app, like a playit.gg address.
	PublicAddress string `json:"publicAddress,omitempty"`
	VMID          int    `json:"vmid,omitempty"`
	Status        string `json:"status,omitempty"`
	// Stats are what the app holds and does right now, when it reports them.
	Stats *agent.AppStats `json:"stats,omitempty"`
	// Operation is the last install, update or removal of this app, while it runs or when it failed.
	Operation *agent.AppInstallStatus `json:"operation"`
	// Rollback says what happened to the container after an update failed.
	Rollback string `json:"rollback,omitempty"`
	// Saved are the answers of a failed install, without secrets, for a retry.
	Saved *agent.SavedAnswers `json:"saved"`
}

type Defaults struct {
	Storages     []string `json:"storages"`
	Storage      string   `json:"storage"`
	JellyfinVMID int      `json:"jellyfinVmid,omitempty"`
	// MediaVMID is the media stack. The forms prefer the Videos share that it
	// or Jellyfin already uses, so both apps see the same media.
	MediaVMID         int    `json:"mediaVmid,omitempty"`
	MoviesFolder      string `json:"moviesFolder"`
	SeriesFolder      string `json:"seriesFolder"`
	AnimeFolder       string `json:"animeFolder"`
	AnimeMoviesFolder string `json:"animeMoviesFolder"`
	// VPNProvider and VPNLocations are where the VPN of the media stack starts.
	VPNProvider       string `json:"vpnProvider"`
	VPNLocations      string `json:"vpnLocations"`
	SubtitleLanguages string `json:"subtitleLanguages"`
	Username          string `json:"username"`
	DownloadsSize     int    `json:"downloadsSize"`
	// Minecraft are the answers that the Minecraft form starts with.
	Minecraft agent.MinecraftAnswers `json:"minecraft"`
}

type View struct {
	Apps     []AppView `json:"apps"`
	Defaults Defaults  `json:"defaults"`
	// Error is set when the host agent did not answer.
	Error string `json:"error,omitempty"`
}

type Options struct {
	Agent   Agent
	Proxmox Proxmox
	// SelfVMID is the manager's container. The agent, and so the new app, runs on its node.
	SelfVMID int
	// Hostname is the name of Iglo-OS, like iglo.local, for the app names below it.
	Hostname string
	// Version is the release of Iglo-OS. A stack of another release can be updated.
	Version string
	Notify  func(ctx context.Context, text string)
	// OnInstalled runs after an install worked, for example to connect a new
	// Jellyfin to the Jellyfin page.
	OnInstalled func(ctx context.Context, app string, status agent.AppInstallStatus) error
	// OnRemoved runs after a removal worked. ip was the address of the container.
	OnRemoved func(ctx context.Context, app string, ip string) error
	Logger    *slog.Logger
	// PollInterval is how often a running install is checked. Tests shorten it.
	PollInterval time.Duration
	// Changed tells the open pages that a topic changed, like events.Apps.
	Changed func(topic string)
}

type Service struct {
	Options

	mu sync.Mutex
	// generation counts the operations, so only the monitor of the newest one runs.
	generation int
	// active is the app of the operation that a monitor still handles, like a rollback after a failed update.
	active    string
	rollbacks map[string]string
	// wake makes the monitor look at a running operation right away.
	wake chan struct{}
	// gamingWake makes WatchGaming look at once, after Play or Stop.
	gamingWake chan struct{}

	// descriptions, addresses and storages change rarely, so the Apps view
	// keeps them for a minute. An operation that finishes forgets them.
	descriptions ttlCache[string]
	addresses    ttlCache[string]
	storages     ttlCache[[]proxmox.BackupStorage]

	gamingMu sync.Mutex
	// gamingSessions are the Moonlight sessions that WatchGaming saw last.
	gamingSessions []agent.GamingSession
	// gamingOwner is the session that came first. The others are stopped.
	gamingOwner string
}

func New(options Options) *Service {
	if options.Notify == nil {
		options.Notify = func(context.Context, string) {}
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.PollInterval == 0 {
		options.PollInterval = 10 * time.Second
	}

	if options.Changed == nil {
		options.Changed = func(string) {}
	}

	return &Service{Options: options, rollbacks: map[string]string{}, wake: make(chan struct{}, 1), gamingWake: make(chan struct{}, 1)}
}

func (s *Service) Status(ctx context.Context) (View, error) {
	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return View{}, err
	}
	node := s.node(resources)

	view := View{Apps: make([]AppView, len(Catalog)), Defaults: s.defaults(ctx, node, resources)}
	install, err := s.Agent.AppInstallStatus(ctx)
	if err != nil {
		view.Error = err.Error()
	}

	// Each app asks Proxmox and the host agent on its own, so they all ask at once.
	var wait sync.WaitGroup
	for index, app := range Catalog {
		wait.Go(func() { view.Apps[index] = s.appView(ctx, app, resources, install) })
	}
	wait.Wait()

	return view, nil
}

// appView is the state of one app on the Apps page.
func (s *Service) appView(ctx context.Context, app App, resources []proxmox.Resource, install agent.AppInstallStatus) AppView {
	appView := AppView{App: app}
	appView.Links = slices.Clone(app.Links)

	if guest, found := findGuest(resources, app); found {
		appView.Installed, appView.VMID, appView.Status = true, guest.VMID, guest.Status
		appView.Managed = hasTags(guest, app)
		if app.ID == "inbox" && slices.Contains(strings.Split(guest.Tags, ";"), "inbox-native") {
			appView.Links = nil
		}
		if appView.Managed && app.Stack {
			description, err := s.descriptions.get(fmt.Sprint(guest.VMID), time.Minute, func() (string, error) {
				return s.Proxmox.ContainerDescription(ctx, guest.Node, guest.VMID)
			})
			if err == nil {
				if app.ID == agent.HeadscaleApp {
					appView.ControllerHost = agent.HeadscaleHostname(description)
				}
				if match := versionPattern.FindStringSubmatch(description); match != nil {
					appView.Version = match[1]
				}
				if match := addressPattern.FindStringSubmatch(description); match != nil {
					appView.PublicAddress = match[1]
				}
				appView.UpdateAvailable = s.Version != "" && appView.Version != s.Version
			}
		}
		if app.Subdomain != "" && s.Hostname != "" {
			appView.HostURL = "https://" + app.Subdomain + "." + s.Hostname
		}
		if ip := s.guestIP(ctx, guest); ip != "" {
			for i, link := range appView.Links {
				appView.Links[i].URL = fmt.Sprintf("http://%s:%d", ip, link.Port)
				if link.Address {
					appView.Links[i].URL = fmt.Sprintf("%s:%d", ip, link.Port)
				}
			}
		}
	}
	s.mu.Lock()
	active := s.active == app.ID
	s.mu.Unlock()
	if install.App == app.ID && (install.State == agent.UpgradeRunning || install.State == agent.UpgradeFailed || active) {
		// The API key of a new Jellyfin is for Iglo-OS only.
		shown := install
		shown.APIKey = ""
		// The operation ends after the monitor handled it, for example after the rollback.
		if active {
			shown.State = agent.UpgradeRunning
		}
		appView.Operation = &shown
		if install.State == agent.UpgradeFailed && install.Action == agent.ActionUpdate {
			s.mu.Lock()
			appView.Rollback = s.rollbacks[app.ID]
			s.mu.Unlock()
		}
	}
	// Stats are best effort: the dashboard shows the app without them.
	if app.Stats && appView.Installed && (appView.Status == "running" || app.HostStats) {
		if stats, err := s.Agent.AppStats(ctx, app.ID, appView.VMID); err == nil {
			appView.Stats = &stats
		}
	}
	if !appView.Installed && !app.Planned && install.State != agent.UpgradeRunning {
		if saved, err := s.Agent.SavedAppAnswers(ctx, app.ID); err == nil {
			appView.Saved = saved
		}
	}

	return appView
}

// Address returns http://<ip>:<port> of an installed app, for a link that works without the Iglo-OS page.
func (s *Service) Address(ctx context.Context, id string, port int) (string, error) {
	index := slices.IndexFunc(Catalog, func(app App) bool { return app.ID == id })
	if index < 0 {
		return "", ErrUnknownApp
	}
	app := Catalog[index]

	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return "", err
	}
	guest, found := findGuest(resources, app)
	if !found {
		return "", fmt.Errorf("the %s is not installed", strings.ToLower(app.Name))
	}
	ip := s.guestIP(ctx, guest)
	if ip == "" {
		return "", fmt.Errorf("container %d of the %s is not running", guest.VMID, strings.ToLower(app.Name))
	}

	return fmt.Sprintf("http://%s:%d", ip, port), nil
}

// node is where the host agent runs: the node of the manager's container.
func (s *Service) node(resources []proxmox.Resource) string {
	online := ""
	for _, resource := range resources {
		if resource.Type == "lxc" && resource.VMID == s.SelfVMID {
			return resource.Node
		}
		if resource.Type == "node" && resource.Status == "online" && online == "" {
			online = resource.Node
		}
	}

	return online
}

func (s *Service) defaults(ctx context.Context, node string, resources []proxmox.Resource) Defaults {
	defaults := Defaults{
		Storages:          []string{},
		MoviesFolder:      "movies",
		SeriesFolder:      "series",
		AnimeFolder:       "anime",
		AnimeMoviesFolder: "anime-movies",
		VPNProvider:       vpn.ProtonVPN,
		VPNLocations:      "Netherlands",
		SubtitleLanguages: "en",
		Username:          "iglo",
		DownloadsSize:     200,
		Minecraft: agent.MinecraftAnswers{
			Memory: 4, MOTD: "A Iglo-OS Minecraft server", Difficulty: "normal", Mode: "survival",
			MaxPlayers: 10, ViewDistance: 10, Whitelist: []string{}, WorldSize: 20,
		},
	}

	storages, err := s.storages.get(node, 5*time.Minute, func() ([]proxmox.BackupStorage, error) {
		return s.Proxmox.ContainerStorages(ctx, node)
	})
	if err == nil {
		for _, storage := range storages {
			if storage.Active {
				defaults.Storages = append(defaults.Storages, storage.Storage)
			}
		}
	}
	slices.Sort(defaults.Storages)
	// local-lvm is the default of a new Proxmox install.
	if slices.Contains(defaults.Storages, "local-lvm") || len(defaults.Storages) == 0 {
		defaults.Storage = "local-lvm"
	} else {
		defaults.Storage = defaults.Storages[0]
	}

	for _, resource := range resources {
		if resource.Type == "lxc" && resource.Name == "jellyfin" {
			defaults.JellyfinVMID = resource.VMID
		}
	}
	if index := slices.IndexFunc(Catalog, func(app App) bool { return app.ID == agent.MediaStackApp }); index >= 0 {
		if guest, found := findGuest(resources, Catalog[index]); found {
			defaults.MediaVMID = guest.VMID
		}
	}

	return defaults
}

// findGuest finds the container of an app: by its Iglo-OS tags, or by name
// for an install that Iglo-OS did not make.
func findGuest(resources []proxmox.Resource, app App) (proxmox.Resource, bool) {
	for _, resource := range resources {
		if resource.Type == "lxc" && resource.Template != 1 && hasTags(resource, app) {
			return resource, true
		}
	}
	for _, resource := range resources {
		if resource.Type == "lxc" && resource.Template != 1 && app.MatchName != "" && resource.Name == app.MatchName {
			return resource, true
		}
	}

	return proxmox.Resource{}, false
}

// hasTags reports whether Iglo-OS installed the container for app.
func hasTags(resource proxmox.Resource, app App) bool {
	tags := strings.Split(resource.Tags, ";")
	return slices.Contains(tags, "iglo") && slices.Contains(tags, app.Tag)
}

// guestIP returns the first IPv4 address of a running container.
func (s *Service) guestIP(ctx context.Context, guest proxmox.Resource) string {
	if guest.Status != "running" {
		return ""
	}
	// A container keeps its address while it runs, so ask Proxmox once a minute.
	ip, _ := s.addresses.get(fmt.Sprint(guest.VMID), time.Minute, func() (string, error) {
		interfaces, err := s.Proxmox.ContainerInterfaces(ctx, guest.Node, guest.VMID)
		if err != nil {
			return "", err
		}
		for _, iface := range interfaces {
			prefix, err := netip.ParsePrefix(iface.Inet)
			if err == nil && iface.Name != "lo" && prefix.Addr().Is4() && !prefix.Addr().IsLoopback() {
				return prefix.Addr().String(), nil
			}
		}
		// No address yet, like right after a start: ask again next time.
		return "", errors.New("no address yet")
	})

	return ip
}

func (s *Service) Install(ctx context.Context, background context.Context, id string, request agent.InstallRequest) error {
	if id == agent.HeadscaleApp {
		if err := request.Headscale.Validate(); err != nil {
			return err
		}
		if err := s.headscaleBudget(ctx); err != nil {
			return err
		}
		return s.start(ctx, background, id, func(app App) error { return s.Agent.InstallApp(ctx, id, request) })
	}
	if index := slices.IndexFunc(Catalog, func(app App) bool { return app.ID == id }); index >= 0 && Catalog[index].Planned {
		return ErrPlanned
	}
	if agent.ScopedApp(id) {
		if err := request.Scoped.ValidateFor(id); err != nil {
			return err
		}
		return s.start(ctx, background, id, func(app App) error { return s.Agent.InstallApp(ctx, app.ID, request) })
	}
	// With KeepSecrets, the agent checks the answers after it adds the saved secrets.
	if !request.KeepSecrets {
		var answers interface{ Validate() error } = request.MediaStackAnswers
		switch id {
		case agent.JellyfinApp:
			answers = request.Jellyfin
		case agent.MinecraftApp:
			answers = request.Minecraft
		}
		if err := answers.Validate(); err != nil {
			return err
		}
	}

	return s.start(ctx, background, id, func(app App) error {
		return s.Agent.InstallApp(ctx, app.ID, request)
	})
}

// Retry runs the install again with the answers the agent saved.
func (s *Service) Retry(ctx context.Context, background context.Context, id string) error {
	if id == agent.HeadscaleApp {
		if err := s.headscaleBudget(ctx); err != nil {
			return err
		}
	}
	return s.start(ctx, background, id, func(app App) error {
		return s.Agent.RetryApp(ctx, app.ID)
	})
}

func (s *Service) Forget(ctx context.Context, id string) error {
	if !slices.ContainsFunc(Catalog, func(app App) bool { return app.ID == id }) {
		return ErrUnknownApp
	}

	return s.Agent.ForgetAppAnswers(ctx, id)
}

// Update uses snapshots for legacy stacks. Scoped apps export app data in
// their installers and require deliberate recovery across guest/NAS boundaries.
func (s *Service) Update(ctx context.Context, background context.Context, id string) error {
	app, guest, err := s.managedGuest(ctx, id)
	if err != nil {
		return err
	}
	if guest.Status != "running" {
		return ErrNotRunning
	}
	if err := s.checkIdle(ctx); err != nil {
		return err
	}

	op := operation{app: app, action: agent.ActionUpdate, guest: guest}
	// Scoped apps publish data outside the guest disk. Rewinding only their
	// local database/queue could lose metadata or repeat already-published jobs.
	// Their installers export app data before updates; recovery is deliberate.
	if !agent.ScopedApp(id) {
		supported, err := s.Proxmox.SnapshotSupported(ctx, guest.Node, guest.VMID)
		if err != nil {
			return fmt.Errorf("could not check snapshot support: %w", err)
		}
		if supported {
			op.snapshot = snapshotPrefix + time.Now().Format("20060102_150405")
			if err := s.Proxmox.CreateSnapshot(ctx, guest.Node, guest.VMID, op.snapshot, "Before app update by iglo"); err != nil {
				return fmt.Errorf("could not create a snapshot, so nothing was updated: %w", err)
			}
		}
	}

	if err := s.Agent.UpdateApp(ctx, app.ID, guest.VMID); err != nil {
		return err
	}
	s.Logger.Info("app update started", "app", id, "vmid", guest.VMID, "snapshot", op.snapshot)
	s.watch(background, op)

	return nil
}

// Remove removes the container of the app with its disks and snapshots. Its backups stay.
func (s *Service) Remove(ctx context.Context, background context.Context, id string) error {
	if id == agent.HeadscaleApp {
		return fmt.Errorf("%w: confirm the controller hostname before removal", agent.ErrInvalidAnswers)
	}
	app, guest, err := s.managedGuest(ctx, id)
	if err != nil {
		return err
	}
	if err := s.checkIdle(ctx); err != nil {
		return err
	}

	op := operation{app: app, action: agent.ActionRemove, guest: guest, ip: s.guestIP(ctx, guest)}
	if err := s.Agent.RemoveApp(ctx, app.ID, guest.VMID); err != nil {
		return err
	}
	s.Logger.Info("app removal started", "app", id, "vmid", guest.VMID)
	s.watch(background, op)

	return nil
}

// VPN returns the VPN settings of the media stack, without its secrets.
func (s *Service) VPN(ctx context.Context, id string) (agent.VPNView, error) {
	_, guest, err := s.mediaStack(ctx, id)
	if err != nil {
		return agent.VPNView{}, err
	}

	return s.Agent.VPN(ctx, guest.VMID)
}

// ChangeVPN gives the media stack new VPN settings. When they do not connect,
// the installer puts the old settings back.
func (s *Service) ChangeVPN(ctx context.Context, background context.Context, id string, settings agent.VPNSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	app, guest, err := s.mediaStack(ctx, id)
	if err != nil {
		return err
	}
	if err := s.checkIdle(ctx); err != nil {
		return err
	}

	if err := s.Agent.ChangeVPN(ctx, guest.VMID, settings); err != nil {
		return err
	}
	s.Logger.Info("vpn change started", "vmid", guest.VMID, "provider", settings.Provider,
		"newSecret", settings.WireGuardPrivateKey != "" || settings.PIAPassword != "" || settings.PIADedicatedToken != "")
	s.watch(background, operation{app: app, action: agent.ActionVPN, guest: guest})

	return nil
}

// Players returns the whitelist and the operators of the Minecraft server.
func (s *Service) Players(ctx context.Context, id string) (agent.MinecraftPlayers, error) {
	_, guest, err := s.runningApp(ctx, id, agent.MinecraftApp)
	if err != nil {
		return agent.MinecraftPlayers{}, err
	}

	return s.Agent.MinecraftPlayers(ctx, guest.VMID)
}

func (s *Service) RecoverPhotos(ctx context.Context, background context.Context, action agent.AppAction) error {
	if action != agent.ActionBackup && action != agent.ActionVerify {
		return agent.ErrInvalidAnswers
	}
	app, guest, err := s.runningApp(ctx, agent.PhotosApp, agent.PhotosApp)
	if err != nil {
		return err
	}
	if err = s.checkIdle(ctx); err != nil {
		return err
	}
	a, ok := s.Agent.(interface {
		RecoverPhotos(context.Context, int, agent.AppAction) error
	})
	if !ok {
		return agent.ErrAppNotAvailable
	}
	if err = a.RecoverPhotos(ctx, guest.VMID, action); err != nil {
		return err
	}
	s.watch(background, operation{app: app, action: action, guest: guest})
	return nil
}

// ChangePlayers gives the Minecraft server a new whitelist and operators,
// without a restart.
func (s *Service) ChangePlayers(ctx context.Context, background context.Context, id string, players agent.MinecraftPlayers) error {
	if err := players.Validate(); err != nil {
		return err
	}
	app, guest, err := s.runningApp(ctx, id, agent.MinecraftApp)
	if err != nil {
		return err
	}
	if err := s.checkIdle(ctx); err != nil {
		return err
	}

	if err := s.Agent.ChangeMinecraftPlayers(ctx, guest.VMID, players); err != nil {
		return err
	}
	s.Logger.Info("change of the minecraft players started", "vmid", guest.VMID, "players", len(players.Whitelist))
	s.watch(background, operation{app: app, action: agent.ActionPlayers, guest: guest})

	return nil
}

// mediaStack finds the running container of the media stack, the only app with a VPN.
func (s *Service) mediaStack(ctx context.Context, id string) (App, proxmox.Resource, error) {
	return s.runningApp(ctx, id, agent.MediaStackApp)
}

// runningApp finds the running container that Iglo-OS installed for id,
// which must be the app want.
func (s *Service) runningApp(ctx context.Context, id, want string) (App, proxmox.Resource, error) {
	if id != want {
		return App{}, proxmox.Resource{}, ErrUnknownApp
	}
	app, guest, err := s.managedGuest(ctx, id)
	if err != nil {
		return App{}, proxmox.Resource{}, err
	}
	if guest.Status != "running" {
		return App{}, proxmox.Resource{}, ErrNotRunning
	}

	return app, guest, nil
}

// managedGuest finds the container that Iglo-OS installed for an app.
func (s *Service) managedGuest(ctx context.Context, id string) (App, proxmox.Resource, error) {
	index := slices.IndexFunc(Catalog, func(app App) bool { return app.ID == id })
	if index < 0 {
		return App{}, proxmox.Resource{}, ErrUnknownApp
	}
	app := Catalog[index]

	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return App{}, proxmox.Resource{}, err
	}
	guest, found := findGuest(resources, app)
	switch {
	case !found:
		return App{}, proxmox.Resource{}, ErrNotInstalled
	case !hasTags(guest, app):
		return App{}, proxmox.Resource{}, ErrNotManaged
	}

	return app, guest, nil
}

func (s *Service) checkIdle(ctx context.Context) error {
	if status, err := s.Agent.AppInstallStatus(ctx); err == nil && status.State == agent.UpgradeRunning {
		return ErrBusy
	}

	return nil
}

// start checks that the app can be installed, runs install, and watches it.
func (s *Service) start(ctx context.Context, background context.Context, id string, install func(App) error) error {
	index := slices.IndexFunc(Catalog, func(app App) bool { return app.ID == id })
	if index < 0 {
		return ErrUnknownApp
	}
	app := Catalog[index]
	if app.Planned {
		return ErrPlanned
	}

	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return err
	}
	if _, found := findGuest(resources, app); found {
		return ErrInstalled
	}
	if err := s.checkIdle(ctx); err != nil {
		return err
	}

	if err := install(app); err != nil {
		return err
	}
	s.Logger.Info("app install started", "app", id)
	s.watch(background, operation{app: app, action: agent.ActionInstall})

	return nil
}

// operation is an install, update or removal that the agent runs.
type operation struct {
	app    App
	action agent.AppAction
	// guest is the container to update or remove.
	guest proxmox.Resource
	// snapshot is the snapshot from before an update, empty when the storage has none.
	snapshot string
	// ip is the address of a container before its removal.
	ip string
}

// watch starts a monitor for op. A monitor of an older operation stops.
func (s *Service) watch(background context.Context, op operation) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.generation++
	s.active = op.app.ID
	delete(s.rollbacks, op.app.ID)
	go s.monitor(background, op, s.generation)
	s.Changed(events.Apps)
}

// Wake makes the monitor look at a running operation now, for example when
// the host agent says that it moved on.
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// monitor waits for the operation to end, rolls back a failed update, and
// sends the result to Telegram.
func (s *Service) monitor(ctx context.Context, op operation, generation int) {
	ticker := time.NewTicker(s.PollInterval)
	defer ticker.Stop()
	defer func() {
		s.mu.Lock()
		if s.generation == generation {
			s.active = ""
		}
		s.mu.Unlock()
		s.Changed(events.Apps)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}

		s.mu.Lock()
		current := s.generation == generation
		s.mu.Unlock()
		if !current {
			return
		}

		status, err := s.Agent.AppInstallStatus(ctx)
		if err != nil || status.State == agent.UpgradeRunning {
			continue
		}
		// The operation changed a container: its version, address or ID.
		s.descriptions.forget()
		s.addresses.forget()

		switch op.action {
		case agent.ActionMigrate:
			s.Changed(events.Inbox)
			s.Notify(ctx, fmt.Sprintf("Inbox migration: %s", status.State))
		case agent.ActionBackup, agent.ActionVerify:
			s.Logger.Info("photos recovery finished", "action", op.action, "state", status.State)
			s.Notify(ctx, fmt.Sprintf("Photos %s: %s", op.action, status.State))
		case agent.ActionUpdate:
			s.finishUpdate(ctx, op, status)
		case agent.ActionRemove:
			s.finishRemove(ctx, op, status)
		case agent.ActionVPN:
			s.finishVPN(ctx, op, status)
		case agent.ActionPlayers:
			// The Players dialog shows the result, so no message.
			s.Logger.Info("minecraft players changed", "vmid", op.guest.VMID, "state", status.State, "message", status.Message)
		default:
			s.finishInstall(ctx, op.app, status)
		}
		return
	}
}

func (s *Service) finishInstall(ctx context.Context, app App, status agent.AppInstallStatus) {
	if status.State != agent.UpgradeSucceeded {
		s.Logger.Warn("app install failed", "app", app.ID, "message", status.Message)
		s.Notify(ctx, fmt.Sprintf("❌ Installing %s failed: %s", app.Name, status.Message))
		return
	}

	s.Logger.Info("app installed", "app", app.ID, "vmid", status.VMID)
	if s.OnInstalled != nil {
		if err := s.OnInstalled(ctx, app.ID, status); err != nil {
			s.Logger.Warn("after the install", "app", app.ID, "error", err)
		}
	}
	s.Notify(ctx, fmt.Sprintf("✅ %s is installed in container %d.", app.Name, status.VMID))
}

func (s *Service) finishUpdate(ctx context.Context, op operation, status agent.AppInstallStatus) {
	vmid := op.guest.VMID
	if status.State == agent.UpgradeSucceeded {
		s.Logger.Info("app updated", "app", op.app.ID, "vmid", vmid)
		s.Notify(ctx, fmt.Sprintf("✅ %s in container %d is updated.", op.app.Name, vmid))
		return
	}

	rollback := "There is no snapshot, because the storage does not support it, so check the container."
	if agent.ScopedApp(op.app.ID) {
		rollback = "Automatic disk rollback is disabled because NAS data is outside the guest. Inspect the failed operation and application backup before recovering."
	}
	localRecovery := op.app.ID == agent.HeadscaleApp && (status.ExitCode == 3 || status.ExitCode == 4)
	if localRecovery {
		rollback = "Controller binaries and state were not replaced. No snapshot rollback was needed."
		if status.ExitCode == 4 {
			rollback = "The previous controller was restored locally and passed health checks. The older snapshot was not applied."
		}
	}
	if op.snapshot != "" && !localRecovery {
		rollback = fmt.Sprintf("Iglo-OS rolled the container back to snapshot %s, from before the update.", op.snapshot)
		if err := s.Proxmox.RollbackSnapshot(ctx, op.guest.Node, vmid, op.snapshot); err != nil {
			rollback = fmt.Sprintf("The rollback to snapshot %s failed too: %v. Roll it back in Proxmox.", op.snapshot, err)
		}
	}
	s.mu.Lock()
	s.rollbacks[op.app.ID] = rollback
	s.mu.Unlock()

	s.Logger.Warn("app update failed", "app", op.app.ID, "vmid", vmid, "message", status.Message, "rollback", rollback)
	s.Notify(ctx, fmt.Sprintf("❌ Updating %s failed: %s. %s", op.app.Name, status.Message, rollback))
}

func (s *Service) finishRemove(ctx context.Context, op operation, status agent.AppInstallStatus) {
	vmid := op.guest.VMID
	if status.State != agent.UpgradeSucceeded {
		s.Logger.Warn("app removal failed", "app", op.app.ID, "vmid", vmid, "message", status.Message)
		s.Notify(ctx, fmt.Sprintf("❌ Removing %s failed: %s", op.app.Name, status.Message))
		return
	}

	s.Logger.Info("app removed", "app", op.app.ID, "vmid", vmid)
	if s.OnRemoved != nil {
		if err := s.OnRemoved(ctx, op.app.ID, op.ip); err != nil {
			s.Logger.Warn("after the removal", "app", op.app.ID, "error", err)
		}
	}
	s.Notify(ctx, fmt.Sprintf("🗑️ %s is removed (container %d). Its backups stay on the Backups page.", op.app.Name, vmid))
}

func (s *Service) finishVPN(ctx context.Context, op operation, status agent.AppInstallStatus) {
	if status.State != agent.UpgradeSucceeded {
		s.Logger.Warn("vpn change failed", "vmid", op.guest.VMID, "message", status.Message)
		s.Notify(ctx, fmt.Sprintf("❌ The new VPN settings of the %s did not connect, so the old settings are back: %s", strings.ToLower(op.app.Name), status.Message))
		return
	}

	s.Logger.Info("vpn changed", "vmid", op.guest.VMID)
	s.Notify(ctx, fmt.Sprintf("✅ The %s uses the new VPN settings.", strings.ToLower(op.app.Name)))
}
