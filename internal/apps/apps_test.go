package apps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
)

type fakeAgent struct {
	mu        sync.Mutex
	status    agent.AppInstallStatus
	installed []string
	retried   []string
	saved     *agent.SavedAnswers
	requests  []agent.InstallRequest
	actions   []string
	stats     int
	// flareSolverr is the switch of the fake media stack, and
	// flareSolverrReads counts how often it was read.
	flareSolverr      bool
	flareSolverrReads int
}

func (f *fakeAgent) FlareSolverr(_ context.Context, vmid int) (agent.FlareSolverrView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flareSolverrReads++
	return agent.FlareSolverrView{Enabled: f.flareSolverr}, nil
}

func (f *fakeAgent) ChangeFlareSolverr(_ context.Context, vmid int, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, fmt.Sprintf("flaresolverr %d %v", vmid, enabled))
	f.status = agent.AppInstallStatus{App: "media", Action: agent.ActionFlareSolverr, State: agent.UpgradeRunning}
	return nil
}

func (f *fakeAgent) UpdateApp(_ context.Context, app string, vmid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, fmt.Sprintf("update %s %d", app, vmid))
	f.status = agent.AppInstallStatus{App: app, Action: agent.ActionUpdate, State: agent.UpgradeRunning}
	return nil
}

func (f *fakeAgent) VPN(_ context.Context, vmid int) (agent.VPNView, error) {
	return agent.VPNView{Provider: "protonvpn", Locations: "Netherlands", HasWireGuardKey: true}, nil
}

func (f *fakeAgent) ChangeVPN(_ context.Context, vmid int, settings agent.VPNSettings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, fmt.Sprintf("vpn %d %s %s", vmid, settings.Provider, settings.Locations))
	f.status = agent.AppInstallStatus{App: "media", Action: agent.ActionVPN, State: agent.UpgradeRunning}
	return nil
}

func (f *fakeAgent) AppStats(_ context.Context, app string, vmid int) (agent.AppStats, error) {
	f.mu.Lock()
	f.stats++
	f.mu.Unlock()
	if app != "music" {
		return agent.AppStats{}, agent.ErrNoStats
	}
	return agent.AppStats{Figures: []agent.Figure{{Label: "Songs", Value: 1534}}, Activity: []agent.Activity{}}, nil
}

func (f *fakeAgent) RemoveApp(_ context.Context, app string, vmid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, fmt.Sprintf("remove %s %d", app, vmid))
	f.status = agent.AppInstallStatus{App: app, Action: agent.ActionRemove, State: agent.UpgradeRunning}
	return nil
}

func (f *fakeAgent) RetryApp(_ context.Context, app string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retried = append(f.retried, app)
	f.status = agent.AppInstallStatus{App: app, State: agent.UpgradeRunning}
	return nil
}

func (f *fakeAgent) SavedAppAnswers(context.Context, string) (*agent.SavedAnswers, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saved, nil
}

func (f *fakeAgent) ForgetAppAnswers(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = nil
	return nil
}

func (f *fakeAgent) InstallApp(_ context.Context, app string, request agent.InstallRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed = append(f.installed, app)
	f.requests = append(f.requests, request)
	f.status = agent.AppInstallStatus{App: app, State: agent.UpgradeRunning}
	return nil
}

func (f *fakeAgent) AppInstallStatus(context.Context) (agent.AppInstallStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *fakeAgent) set(status agent.AppInstallStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

type fakeProxmox struct {
	mu          sync.Mutex
	resources   []proxmox.Resource
	description string
	noSnapshots bool
	snapshots   []string
	rollbacks   []string
}

func (f *fakeProxmox) ContainerDescription(context.Context, string, int) (string, error) {
	return f.description, nil
}

func (f *fakeProxmox) SnapshotSupported(context.Context, string, int) (bool, error) {
	return !f.noSnapshots, nil
}

func (f *fakeProxmox) CreateSnapshot(_ context.Context, _ string, vmid int, name, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots = append(f.snapshots, name)
	return nil
}

func (f *fakeProxmox) RollbackSnapshot(_ context.Context, _ string, vmid int, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rollbacks = append(f.rollbacks, fmt.Sprintf("%d %s", vmid, name))
	return nil
}

func (f *fakeProxmox) Resources(context.Context) ([]proxmox.Resource, error) {
	return f.resources, nil
}

func (f *fakeProxmox) Bridges(context.Context, string) ([]proxmox.Bridge, error) {
	return []proxmox.Bridge{{Iface: "vmbr1", CIDR: "10.0.0.1/24"}, {Iface: "vmbr0", CIDR: "192.168.1.10/24", Gateway: "192.168.1.1"}}, nil
}

func (f *fakeProxmox) ContainerStorages(context.Context, string) ([]proxmox.BackupStorage, error) {
	return []proxmox.BackupStorage{
		{Storage: "local-lvm", Active: true},
		{Storage: "fast", Active: true},
		{Storage: "offline", Active: false},
	}, nil
}

func (f *fakeProxmox) ContainerInterfaces(_ context.Context, _ string, vmid int) ([]proxmox.Interface, error) {
	return []proxmox.Interface{{Name: "lo", Inet: "127.0.0.1/8"}, {Name: "eth0", Inet: "192.168.1.50/24"}}, nil
}

func validAnswers() agent.MediaStackAnswers {
	return agent.MediaStackAnswers{
		VideosBinding: "videos", MoviesFolder: "movies", SeriesFolder: "series",
		VPN:               agent.VPNSettings{Provider: "protonvpn", Locations: "Netherlands", WireGuardPrivateKey: "cGVyZmVjdGx5IHZhbGlkIGtleSBvZiAzMiBieXRlcyE="},
		SubtitleLanguages: "en",
		Username:          "iglo", Password: "correct horse battery",
		Storage: "local-lvm", DownloadsSize: 200,
	}
}

func appByID(t *testing.T, view View, id string) AppView {
	t.Helper()
	for _, app := range view.Apps {
		if app.ID == id {
			return app
		}
	}
	t.Fatalf("no app %q in %+v", id, view.Apps)
	return AppView{}
}

func baseResources() []proxmox.Resource {
	return []proxmox.Resource{
		{Type: "node", Node: "pve", Status: "online"},
		{Type: "lxc", Node: "pve", VMID: 100, Name: "iglo", Status: "running", Tags: "iglo"},
		{Type: "lxc", Node: "pve", VMID: 110, Name: "jellyfin", Status: "running"},
	}
}

func TestStatusBeforeInstall(t *testing.T) {
	service := New(Options{Agent: &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100})

	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	media := appByID(t, view, "media")
	if media.ID != "media" || media.Installed || media.Operation != nil || media.Links[0].URL != "" {
		t.Fatalf("media = %+v", media)
	}
	defaults := view.Defaults
	if defaults.Storage != "local-lvm" || len(defaults.Storages) != 2 || defaults.JellyfinVMID != 110 {
		t.Fatalf("defaults = %+v", defaults)
	}
	// The Headscale form starts from the host's own network on vmbr0.
	if defaults.LANAddress != "192.168.1.10/24" || defaults.LANGateway != "192.168.1.1" {
		t.Fatalf("network = %q %q", defaults.LANAddress, defaults.LANGateway)
	}
}

func TestStatusAfterInstall(t *testing.T) {
	resources := append(baseResources(), proxmox.Resource{Type: "lxc", Node: "pve", VMID: 130, Name: "media", Status: "running", Tags: "iglo;media"})
	fake := &fakeAgent{status: agent.AppInstallStatus{App: "media", State: agent.UpgradeSucceeded, VMID: 130}}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: resources}, SelfVMID: 100, Hostname: "iglo.local"})

	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	media := appByID(t, view, "media")
	if !media.Installed || media.VMID != 130 || media.Operation != nil || media.HostURL != "https://seerr.iglo.local" {
		t.Fatalf("media = %+v", media)
	}

	// An install that finished lately shows, so the page can say it worked.
	fake.status.FinishedAt = time.Now().Add(-5 * time.Minute)
	if media := appByID(t, mustStatus(t, service), "media"); media.Operation == nil || media.Operation.State != agent.UpgradeSucceeded {
		t.Fatalf("recent install = %+v", media.Operation)
	}
	fake.status.FinishedAt = time.Now().Add(-2 * time.Hour)
	if media := appByID(t, mustStatus(t, service), "media"); media.Operation != nil {
		t.Fatalf("old install = %+v", media.Operation)
	}
	if media.Links[0].URL != "http://192.168.1.50:5055" || media.Links[5].URL != "http://192.168.1.50:8080" {
		t.Fatalf("links = %+v", media.Links)
	}
	// The catalog itself must not change.
	if Catalog[0].Links[0].URL != "" {
		t.Fatal("the catalog got a URL")
	}

	if err := service.Install(context.Background(), context.Background(), "media", agent.InstallRequest{MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrInstalled) {
		t.Fatalf("install again: %v", err)
	}
}

func TestInstallChecksAndNotifies(t *testing.T) {
	fake := &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}
	messages := make(chan string, 1)
	service := New(Options{
		Agent: fake, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100,
		Notify:       func(_ context.Context, text string) { messages <- text },
		PollInterval: 10 * time.Millisecond,
	})
	ctx := context.Background()

	if err := service.Install(ctx, ctx, "nextcloud", agent.InstallRequest{MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrUnknownApp) {
		t.Fatalf("unknown: %v", err)
	}
	bad := validAnswers()
	bad.Password = "short"
	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: bad}); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Fatalf("invalid: %v", err)
	}

	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: validAnswers()}); err != nil {
		t.Fatal(err)
	}
	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: validAnswers()}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second install: %v", err)
	}

	view, _ := service.Status(ctx)
	if appByID(t, view, "media").Operation == nil || appByID(t, view, "media").Operation.State != agent.UpgradeRunning {
		t.Fatalf("install = %+v", appByID(t, view, "media").Operation)
	}

	fake.set(agent.AppInstallStatus{App: "media", State: agent.UpgradeFailed, Message: "the VPN did not connect"})
	select {
	case message := <-messages:
		if !strings.Contains(message, "Installing Media stack failed: the VPN did not connect") {
			t.Fatalf("message = %q", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
	}
}

func TestRetryAndChangedAnswers(t *testing.T) {
	saved := &agent.SavedAnswers{Answers: agent.MediaStackAnswers{VideosBinding: "videos"}}
	fake := &fakeAgent{status: agent.AppInstallStatus{App: "media", State: agent.UpgradeFailed}, saved: saved}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100, PollInterval: time.Hour})
	ctx := context.Background()

	view, err := service.Status(ctx)
	if err != nil || appByID(t, view, "media").Saved != saved {
		t.Fatalf("saved = %+v, err = %v", appByID(t, view, "media").Saved, err)
	}

	if err := service.Retry(ctx, ctx, "media"); err != nil || len(fake.retried) != 1 {
		t.Fatalf("retry: %v, %v", err, fake.retried)
	}
	if err := service.Retry(ctx, ctx, "media"); !errors.Is(err, ErrBusy) {
		t.Fatalf("retry while running: %v", err)
	}

	// With KeepSecrets the empty secrets are fine: the agent fills them in.
	fake.set(agent.AppInstallStatus{App: "media", State: agent.UpgradeFailed})
	changed := validAnswers()
	changed.Password, changed.VPN.WireGuardPrivateKey = "", ""
	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: changed, KeepSecrets: true}); err != nil {
		t.Fatal(err)
	}
	if !fake.requests[0].KeepSecrets {
		t.Fatal("KeepSecrets was not passed on")
	}
	// Without it, they are required.
	fake.set(agent.AppInstallStatus{App: "media", State: agent.UpgradeFailed})
	if err := service.Install(ctx, ctx, "media", agent.InstallRequest{MediaStackAnswers: changed}); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Fatalf("empty secrets: %v", err)
	}

	if err := service.Forget(ctx, "media"); err != nil || fake.saved != nil {
		t.Fatalf("forget: %v", err)
	}
	if err := service.Forget(ctx, "nextcloud"); !errors.Is(err, ErrUnknownApp) {
		t.Fatalf("forget unknown: %v", err)
	}
}

func TestAddress(t *testing.T) {
	resources := append(baseResources(), proxmox.Resource{Type: "lxc", Node: "pve", VMID: 130, Name: "media", Status: "running", Tags: "iglo;media"})
	service := New(Options{Agent: &fakeAgent{}, Proxmox: &fakeProxmox{resources: resources}, SelfVMID: 100})

	address, err := service.Address(context.Background(), "media", 5055)
	if err != nil || address != "http://192.168.1.50:5055" {
		t.Fatalf("address = %q, err = %v", address, err)
	}

	service = New(Options{Agent: &fakeAgent{}, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100})
	if _, err := service.Address(context.Background(), "media", 5055); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err = %v", err)
	}
}

func TestJellyfinFromCommunityScriptsCountsAsInstalled(t *testing.T) {
	// baseResources has a container named jellyfin without Iglo-OS tags.
	fake := &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100})

	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	jellyfin := appByID(t, view, "jellyfin")
	if !jellyfin.Installed || jellyfin.VMID != 110 || jellyfin.Links[0].URL != "http://192.168.1.50:8096" {
		t.Fatalf("jellyfin = %+v", jellyfin)
	}
	ctx := context.Background()
	if err := service.Install(ctx, ctx, "jellyfin", agent.InstallRequest{Jellyfin: validJellyfin()}); !errors.Is(err, ErrInstalled) {
		t.Fatalf("second Jellyfin: %v", err)
	}
}

func validJellyfin() agent.JellyfinAnswers {
	return agent.JellyfinAnswers{
		VideosBinding: "videos", MoviesFolder: "movies", SeriesFolder: "series",
		AdminUsername: "dries", AdminPassword: "jelly pass", Theme: true, Storage: "local-lvm",
	}
}

func TestJellyfinInstallConnectsIglo(t *testing.T) {
	resources := []proxmox.Resource{
		{Type: "node", Node: "pve", Status: "online"},
		{Type: "lxc", Node: "pve", VMID: 100, Name: "iglo", Status: "running", Tags: "iglo"},
	}
	fake := &fakeAgent{
		status: agent.AppInstallStatus{State: agent.UpgradeIdle},
	}
	connected := make(chan agent.AppInstallStatus, 1)
	service := New(Options{
		Agent: fake, Proxmox: &fakeProxmox{resources: resources}, SelfVMID: 100, PollInterval: 10 * time.Millisecond,
		OnInstalled: func(_ context.Context, app string, status agent.AppInstallStatus) error {
			if app == "jellyfin" {
				connected <- status
			}
			return nil
		},
	})
	ctx := context.Background()

	view, _ := service.Status(ctx)

	bad := validJellyfin()
	bad.AdminPassword = ""
	if err := service.Install(ctx, ctx, "jellyfin", agent.InstallRequest{Jellyfin: bad}); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Fatalf("no password: %v", err)
	}
	if err := service.Install(ctx, ctx, "jellyfin", agent.InstallRequest{Jellyfin: validJellyfin()}); err != nil {
		t.Fatal(err)
	}

	done := agent.AppInstallStatus{App: "jellyfin", State: agent.UpgradeSucceeded, VMID: 140, IP: "192.168.1.60", APIKey: "secret-key"}
	fake.set(done)
	select {
	case status := <-connected:
		if status.APIKey != "secret-key" || status.IP != "192.168.1.60" {
			t.Fatalf("status = %+v", status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnInstalled did not run")
	}

	// The key never goes to the browser.
	fake.set(agent.AppInstallStatus{App: "jellyfin", State: agent.UpgradeFailed, APIKey: "secret-key"})
	view, _ = service.Status(ctx)
	if install := appByID(t, view, "jellyfin").Operation; install == nil || install.APIKey != "" {
		t.Fatalf("install = %+v", install)
	}
}

// The step after an install runs once it worked, and is dropped when it fails.
func TestInstallThenRunsOnlyAfterSuccess(t *testing.T) {
	ctx := context.Background()
	for _, state := range []agent.UpgradeState{agent.UpgradeSucceeded, agent.UpgradeFailed} {
		resources := []proxmox.Resource{{Type: "node", Node: "pve", Status: "online"}}
		fake := &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}
		ran := make(chan agent.AppInstallStatus, 1)
		service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: resources}, SelfVMID: 100, PollInterval: 10 * time.Millisecond})
		then := func(_ context.Context, status agent.AppInstallStatus) error { ran <- status; return nil }
		if err := service.InstallThen(ctx, ctx, "jellyfin", agent.InstallRequest{Jellyfin: validJellyfin()}, then); err != nil {
			t.Fatal(err)
		}
		fake.set(agent.AppInstallStatus{App: "jellyfin", State: state, VMID: 140, IP: "192.168.1.60"})
		select {
		case status := <-ran:
			if state != agent.UpgradeSucceeded || status.IP != "192.168.1.60" {
				t.Fatalf("%s: ran with %+v", state, status)
			}
		case <-time.After(500 * time.Millisecond):
			if state == agent.UpgradeSucceeded {
				t.Fatal("did not run after the install")
			}
		}
	}
}

func mediaResources(tags string) []proxmox.Resource {
	return append(baseResources(), proxmox.Resource{Type: "lxc", Node: "pve", VMID: 130, Name: "media", Status: "running", Tags: tags})
}

func TestStatusShowsTheStackVersion(t *testing.T) {
	pve := &fakeProxmox{resources: mediaResources("iglo;media"), description: "Iglo-OS media stack\niglo-version: v0.7.1\n"}
	service := New(Options{Agent: &fakeAgent{}, Proxmox: pve, SelfVMID: 100, Version: "v0.8.0"})

	view, _ := service.Status(context.Background())
	media := appByID(t, view, "media")
	if !media.Managed || media.Version != "v0.7.1" || !media.UpdateAvailable {
		t.Fatalf("media = %+v", media)
	}
	// Jellyfin from community-scripts is not Iglo-OS's.
	if jellyfin := appByID(t, view, "jellyfin"); jellyfin.Managed || jellyfin.UpdateAvailable {
		t.Fatalf("jellyfin = %+v", jellyfin)
	}

	// The notes are kept for a minute, until an operation finishes, like this update.
	pve.description = "Iglo-OS media stack\niglo-version: v0.8.0\n"
	if media := appByID(t, mustStatus(t, service), "media"); !media.UpdateAvailable {
		t.Fatal("asked Proxmox again within the minute")
	}
	service.descriptions.forget()
	view, _ = service.Status(context.Background())
	if media := appByID(t, view, "media"); media.UpdateAvailable {
		t.Fatalf("same version: %+v", media)
	}
}

func TestUpdateOnlyForAppsOfIglo(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAgent{}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100, PollInterval: time.Hour})

	if err := service.Update(ctx, ctx, "media"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("not installed: %v", err)
	}
	// baseResources has a Jellyfin without the tags of Iglo-OS.
	if err := service.Remove(ctx, ctx, "jellyfin"); !errors.Is(err, ErrNotManaged) {
		t.Fatalf("jellyfin by name: %v", err)
	}
	stopped := mediaResources("iglo;media")
	stopped[len(stopped)-1].Status = "stopped"
	service = New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: stopped}, SelfVMID: 100, PollInterval: time.Hour})
	if err := service.Update(ctx, ctx, "media"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stopped: %v", err)
	}
	if len(fake.actions) != 0 {
		t.Fatalf("actions = %v", fake.actions)
	}
}

func TestFailedUpdateRollsBack(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAgent{}
	pve := &fakeProxmox{resources: mediaResources("iglo;media")}
	messages := make(chan string, 1)
	service := New(Options{
		Agent: fake, Proxmox: pve, SelfVMID: 100, PollInterval: 10 * time.Millisecond,
		Notify: func(_ context.Context, text string) { messages <- text },
	})

	if err := service.Update(ctx, ctx, "media"); err != nil {
		t.Fatal(err)
	}
	if err := service.Remove(ctx, ctx, "media"); !errors.Is(err, ErrBusy) {
		t.Fatalf("remove during the update: %v", err)
	}
	if len(pve.snapshots) != 1 || !strings.HasPrefix(pve.snapshots[0], "iglo_") || fake.actions[0] != "update media 130" {
		t.Fatalf("snapshots = %v, actions = %v", pve.snapshots, fake.actions)
	}

	fake.set(agent.AppInstallStatus{App: "media", Action: agent.ActionUpdate, State: agent.UpgradeFailed, Message: "the update stopped with exit code 1"})
	select {
	case message := <-messages:
		if !strings.Contains(message, "Updating Media stack failed") || !strings.Contains(message, "rolled the container back") {
			t.Fatalf("message = %q", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
	}
	pve.mu.Lock()
	rollbacks := pve.rollbacks
	pve.mu.Unlock()
	if len(rollbacks) != 1 || rollbacks[0] != "130 "+pve.snapshots[0] {
		t.Fatalf("rollbacks = %v", rollbacks)
	}

	view, _ := service.Status(ctx)
	if media := appByID(t, view, "media"); media.Operation == nil || !strings.Contains(media.Rollback, pve.snapshots[0]) {
		t.Fatalf("media = %+v", media)
	}
}

func TestRemoveDisconnects(t *testing.T) {
	ctx := context.Background()
	resources := append(baseResources()[:2], proxmox.Resource{Type: "lxc", Node: "pve", VMID: 140, Name: "jellyfin", Status: "running", Tags: "iglo;jellyfin"})
	fake := &fakeAgent{}
	removed := make(chan string, 1)
	service := New(Options{
		Agent: fake, Proxmox: &fakeProxmox{resources: resources}, SelfVMID: 100, PollInterval: 10 * time.Millisecond,
		OnRemoved: func(_ context.Context, app, ip string) error {
			removed <- app + " " + ip
			return nil
		},
	})

	if err := service.Remove(ctx, ctx, "jellyfin"); err != nil {
		t.Fatal(err)
	}
	fake.set(agent.AppInstallStatus{App: "jellyfin", Action: agent.ActionRemove, State: agent.UpgradeSucceeded})
	select {
	case got := <-removed:
		if got != "jellyfin 192.168.1.50" {
			t.Fatalf("removed = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnRemoved did not run")
	}
}

func TestChangeVPN(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAgent{}
	messages := make(chan string, 1)
	service := New(Options{
		Agent: fake, Proxmox: &fakeProxmox{resources: mediaResources("iglo;media")}, SelfVMID: 100, PollInterval: 10 * time.Millisecond,
		Notify: func(_ context.Context, text string) { messages <- text },
	})

	if view, err := service.VPN(ctx, "media"); err != nil || view.Locations != "Netherlands" {
		t.Fatalf("view = %+v, err = %v", view, err)
	}
	if _, err := service.VPN(ctx, "jellyfin"); !errors.Is(err, ErrUnknownApp) {
		t.Fatalf("jellyfin has no VPN: %v", err)
	}
	if err := service.ChangeVPN(ctx, ctx, "media", agent.VPNSettings{Provider: "protonvpn", Locations: "NL1"}); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Fatalf("bad countries: %v", err)
	}

	if err := service.ChangeVPN(ctx, ctx, "media", agent.VPNSettings{Provider: "protonvpn", Locations: "Switzerland"}); err != nil {
		t.Fatal(err)
	}
	if fake.actions[0] != "vpn 130 protonvpn Switzerland" {
		t.Fatalf("actions = %v", fake.actions)
	}

	fake.set(agent.AppInstallStatus{App: "media", Action: agent.ActionVPN, State: agent.UpgradeFailed, Message: "the vpn stopped with exit code 1"})
	select {
	case message := <-messages:
		if !strings.Contains(message, "old settings are back") {
			t.Fatalf("message = %q", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
	}
}

func TestChangeFlareSolverr(t *testing.T) {
	ctx := context.Background()
	fake := &fakeAgent{flareSolverr: true, status: agent.AppInstallStatus{State: agent.UpgradeIdle}}
	messages := make(chan string, 1)
	service := New(Options{
		Agent: fake, Proxmox: &fakeProxmox{resources: mediaResources("iglo;media")}, SelfVMID: 100, PollInterval: 10 * time.Millisecond,
		Notify: func(_ context.Context, text string) { messages <- text },
	})
	flareSolverr := func() (*bool, int) {
		view, err := service.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return appByID(t, view, "media").FlareSolverr, fake.flareSolverrReads
	}

	// The card says whether it runs, and the Apps view keeps that.
	flareSolverr()
	if on, reads := flareSolverr(); on == nil || !*on || reads != 1 {
		t.Fatalf("flareSolverr = %v, reads = %d", on, reads)
	}
	if err := service.ChangeFlareSolverr(ctx, ctx, "jellyfin", false); !errors.Is(err, ErrUnknownApp) {
		t.Fatalf("jellyfin has no FlareSolverr: %v", err)
	}

	if err := service.ChangeFlareSolverr(ctx, ctx, "media", false); err != nil {
		t.Fatal(err)
	}
	if fake.actions[0] != "flaresolverr 130 false" {
		t.Fatalf("actions = %v", fake.actions)
	}
	if err := service.ChangeFlareSolverr(ctx, ctx, "media", true); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second change while one runs: %v", err)
	}
	// While it runs, the view doesn't read the switch.
	if on, reads := flareSolverr(); on != nil || reads != 1 {
		t.Fatalf("while running: flareSolverr = %v, reads = %d", on, reads)
	}

	fake.mu.Lock()
	fake.flareSolverr = false
	fake.mu.Unlock()
	fake.set(agent.AppInstallStatus{App: "media", Action: agent.ActionFlareSolverr, State: agent.UpgradeSucceeded, FinishedAt: time.Now()})
	select {
	case message := <-messages:
		if message != "✅ FlareSolverr is off in the media stack." {
			t.Fatalf("message = %q", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no message")
	}
	// The finished operation forgot the old answer.
	if on, reads := flareSolverr(); on == nil || *on || reads != 2 {
		t.Fatalf("after the change: flareSolverr = %v, reads = %d", on, reads)
	}
}

func TestScopedAppsHaveInstallersAndRequireBindings(t *testing.T) {
	fake := &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100})

	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"music", "photos", "inbox"} {
		if app := appByID(t, view, id); app.Planned || app.Installed || app.Stats != nil || app.Saved != nil {
			t.Errorf("%s = %+v", id, app)
		}
	}
	if err := service.Install(context.Background(), context.Background(), "music", agent.InstallRequest{}); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Fatalf("missing storage bindings: %v", err)
	}
}

func TestStatusHasTheStatsOfARunningApp(t *testing.T) {
	resources := append(baseResources(),
		proxmox.Resource{Type: "lxc", Node: "pve", VMID: 160, Name: "music", Status: "running", Tags: "iglo;music"},
		proxmox.Resource{Type: "lxc", Node: "pve", VMID: 170, Name: "photos", Status: "stopped", Tags: "iglo;photos"},
	)
	fake := &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}
	service := New(Options{Agent: fake, Proxmox: &fakeProxmox{resources: resources}, SelfVMID: 100})

	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if music := appByID(t, view, "music"); music.Stats == nil || music.Stats.Figures[0].Value != 1534 {
		t.Fatalf("music = %+v", music)
	}
	// The next view, like during an install, uses the stats it has.
	if music := appByID(t, mustStatus(t, service), "music"); music.Stats == nil || fake.stats != 1 {
		t.Fatalf("music = %+v after %d stats calls", music, fake.stats)
	}
	// A stopped app has no stats, and an app without stats still shows.
	if photos := appByID(t, view, "photos"); !photos.Installed || photos.Stats != nil {
		t.Fatalf("photos = %+v", photos)
	}
}

func mustStatus(t *testing.T, service *Service) View {
	t.Helper()
	view, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return view
}
