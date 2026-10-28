package setup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"iglo/internal/agent"
	"iglo/internal/apps"
	"iglo/internal/backups"
	"iglo/internal/workloads"
)

// fakeHost plays the host agent, the apps and the backup job.
type fakeHost struct {
	mu        sync.Mutex
	bindings  []agent.Binding
	installed map[string]bool
	requests  map[string]agent.InstallRequest
	order     []string
	failApp   string
	job       agent.BackupJob
	jobSaved  bool
	backupsOn string
	settings  workloads.Settings
}

func (f *fakeHost) StorageBindings(context.Context) (agent.BindingView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return agent.BindingView{Bindings: slices.Clone(f.bindings)}, nil
}

func (f *fakeHost) CreateBinding(_ context.Context, in agent.BindingInput) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bindings = append(f.bindings, agent.Binding{ID: in.ID, Role: in.Role, ReadOnly: in.ReadOnly, Mounted: true, Source: "//" + in.Server + "/" + in.Share})
	return nil
}

func (f *fakeHost) UseBindingForBackups(_ context.Context, id string, on bool) error {
	f.backupsOn = id
	return nil
}

func (f *fakeHost) HostMemory(context.Context) (agent.HostMemory, error) {
	return agent.HostMemory{Node: "pve", TotalBytes: 32 << 30}, nil
}

func (f *fakeHost) GamingReady(context.Context) (agent.GamingReadiness, error) {
	return agent.GamingReadiness{Missing: []string{"the uinput module (/dev/uinput)"}}, nil
}

func (f *fakeHost) Status(context.Context) (apps.View, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	view := apps.View{Defaults: apps.Defaults{Storage: "local-lvm", Storages: []string{"local-lvm"}}}
	for _, id := range Apps {
		view.Apps = append(view.Apps, apps.AppView{App: apps.App{ID: id, Name: id}, Installed: f.installed[id]})
	}
	return view, nil
}

func (f *fakeHost) Install(_ context.Context, _ context.Context, id string, request agent.InstallRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id == f.failApp {
		return errors.New("the NAS refused the account")
	}
	f.order = append(f.order, id)
	f.requests[id] = request
	f.installed[id] = true
	return nil
}

func (f *fakeHost) SaveJob(_ context.Context, job agent.BackupJob) error {
	f.job, f.jobSaved = job, true
	return nil
}

func (f *fakeHost) SaveSettings(settings workloads.Settings) error {
	f.settings = settings
	return nil
}

func (f *fakeHost) workloadsStatus(url string) workloads.View {
	return workloads.View{Settings: workloads.Settings{URL: url}}
}

// fakeBackups and fakeWorkloads adapt fakeHost to the interfaces that share method names.
type fakeBackups struct{ *fakeHost }

func (f fakeBackups) Status(context.Context) (backups.View, error) { return backups.View{}, nil }

type fakeWorkloads struct {
	*fakeHost
	url string
}

func (f fakeWorkloads) Status() workloads.View { return f.workloadsStatus(f.url) }

func validPlan() Plan {
	share := func(name, user string) Share {
		return Share{Share: name, Account: Account{Username: user, Password: "nas-" + user}}
	}
	return Plan{
		Apps: []string{agent.JellyfinApp, agent.MediaStackApp, agent.PhotosApp},
		NAS: NAS{Server: "192.168.1.20", Shares: map[string]Share{
			"videos": share("Videos", "media"), "photos": share("Photos", "photos"), "backups": share("Backups", "backup"),
		}},
		Admin:          Admin{Username: "nick", Password: "correct horse battery", Email: "nick@example.com"},
		VPN:            VPN{Provider: "protonvpn", Locations: "Netherlands", WireGuardKey: "cGVyZmVjdGx5IHZhbGlkIGtleSBvZiAzMiBieXRlcyE="},
		FlareSolverr:   true,
		AnimeIndexers:  true,
		ProxmoxBackups: true,
		Timezone:       "Europe/Brussels",
		Storage:        "local-lvm",
	}
}

func TestPlanValidate(t *testing.T) {
	if err := validPlan().Validate(nil); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Plan){
		"no apps":         func(p *Plan) { p.Apps = nil },
		"unknown app":     func(p *Plan) { p.Apps = []string{"minecraft"} },
		"short password":  func(p *Plan) { p.Admin.Password = "short" },
		"bad username":    func(p *Plan) { p.Admin.Username = "nick b" },
		"no email":        func(p *Plan) { p.Admin.Email = "" },
		"no share":        func(p *Plan) { delete(p.NAS.Shares, "photos") },
		"no account":      func(p *Plan) { p.NAS.Shares["videos"] = Share{Share: "Videos"} },
		"bad VPN key":     func(p *Plan) { p.VPN.WireGuardKey = "nope" },
		"unknown country": func(p *Plan) { p.VPN.Locations = "Atlantis" },
		"PIA no password": func(p *Plan) { p.VPN.Provider, p.VPN.PIAUsername = "pia", "p1234567" },
		"gaming, no ok":   func(p *Plan) { p.Apps = append(p.Apps, agent.GamingApp) },
		"bad time zone":   func(p *Plan) { p.Timezone = "Mars/Base" },
		"no NAS address":  func(p *Plan) { p.NAS.Server = "" },
		"one empty login": func(p *Plan) { p.NAS.OneAccount = true },
	} {
		plan := validPlan()
		plan.NAS.Shares = map[string]Share{}
		for role, share := range validPlan().NAS.Shares {
			plan.NAS.Shares[role] = share
		}
		change(&plan)
		if err := plan.Validate(nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// One account for all shares needs only the share names.
	plan := validPlan()
	plan.NAS.OneAccount = true
	plan.NAS.Account = Account{Username: "umbrel", Password: "secret"}
	for role, share := range plan.NAS.Shares {
		plan.NAS.Shares[role] = Share{Share: share.Share}
	}
	if err := plan.Validate(nil); err != nil {
		t.Fatal(err)
	}
	// A share that is connected needs no answers.
	plan = validPlan()
	delete(plan.NAS.Shares, "videos")
	if err := plan.Validate(map[string]bool{"videos": true}); err != nil {
		t.Fatal(err)
	}
}

func fakeImmich(t *testing.T) (*httptest.Server, *[]string) {
	calls := []string{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /api/server/config":
			w.Write([]byte(`{"isInitialized":false}`))
		case "POST /api/auth/admin-sign-up":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["email"] != "nick@example.com" || body["name"] != "nick" {
				http.Error(w, "bad sign-up", 400)
			}
		case "POST /api/auth/login":
			w.Write([]byte(`{"accessToken":"token"}`))
		case "GET /api/admin/config":
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Error("configuration requires the ephemeral admin session")
			}
			w.Write([]byte(`{"job":{"videoConversion":{"concurrency":2},"faceDetection":{"concurrency":2},"smartSearch":{"concurrency":2},"ocr":{"concurrency":1}},"ffmpeg":{"threads":0,"transcode":"optimal"},"machineLearning":{"facialRecognition":{"enabled":true,"modelName":"buffalo_l"}},"unrelated":{"keep":true}}`))
		case "PUT /api/admin/config":
			var config map[string]any
			json.NewDecoder(r.Body).Decode(&config)
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Error("missing ephemeral admin token")
			}
			if config["unrelated"].(map[string]any)["keep"] != true {
				t.Error("unrelated settings lost")
			}
		case "POST /api/api-keys":
			var body struct {
				Permissions []string `json:"permissions"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if r.Header.Get("Authorization") != "Bearer token" || !slices.Equal(body.Permissions, []string{"queue.read", "queue.update"}) {
				http.Error(w, "bad key request", 400)
				return
			}
			w.Write([]byte(`{"secret":"immich-key"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func waitFor(t *testing.T, service *Service, state string) View {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		view := service.View(context.Background())
		if view.State == state {
			return view
		}
		if time.Now().After(deadline) {
			t.Fatalf("state = %s, steps = %+v", view.State, view.Steps)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSetupRunsThePlanInOrder(t *testing.T) {
	immich, calls := fakeImmich(t)
	host := &fakeHost{installed: map[string]bool{}, requests: map[string]agent.InstallRequest{}}
	// A Videos share that was connected before is used again.
	host.bindings = []agent.Binding{{ID: "films", Role: "videos", Mounted: true}}
	service := New(Options{
		Apps: host, Agent: host, Backups: fakeBackups{host}, Workloads: fakeWorkloads{host, immich.URL},
		JellyfinKey: func() string { return "0123456789abcdef0123456789abcdef" }, Poll: time.Millisecond,
	})

	if err := service.Start(context.Background(), validPlan()); err != nil {
		t.Fatal(err)
	}
	view := waitFor(t, service, StateDone)
	ids := []string{}
	for _, step := range view.Steps {
		ids = append(ids, step.ID)
		if step.State != StepDone {
			t.Errorf("%s: %s %s", step.ID, step.State, step.Message)
		}
	}
	want := []string{"share:videos", "share:photos", "share:backups", "backups", "app:jellyfin", "app:media", "app:photos", "immich"}
	if !slices.Equal(ids, want) {
		t.Fatalf("steps = %v", ids)
	}
	if !slices.Equal(host.order, []string{agent.JellyfinApp, agent.MediaStackApp, agent.PhotosApp}) {
		t.Fatalf("order = %v", host.order)
	}

	// The apps use the shares and each other.
	if got := host.requests[agent.JellyfinApp].Jellyfin; got.VideosBinding != "films" || got.AdminUsername != "nick" {
		t.Fatalf("jellyfin = %+v", got)
	}
	media := host.requests[agent.MediaStackApp].MediaStackAnswers
	if media.VideosBinding != "films" || media.JellyfinAPIKey != "0123456789abcdef0123456789abcdef" || media.JellyfinAdminUsername != "nick" || !media.AnimeIndexers {
		t.Fatalf("media = %+v", media)
	}
	if got := host.requests[agent.PhotosApp].Scoped; got.LibraryBinding != "photos" || got.BackupBinding != "backups" || got.MemoryMiB != 8192 {
		t.Fatalf("photos = %+v", got)
	}
	if host.backupsOn != "backups" || host.job.Storage != "iglo-backups" || host.job.Hour != 3 {
		t.Fatalf("backups on %q, job = %+v", host.backupsOn, host.job)
	}
	// Each share got its own account.
	for _, binding := range host.bindings[1:] {
		if !strings.HasPrefix(binding.Source, "//192.168.1.20/") {
			t.Fatalf("binding = %+v", binding)
		}
	}

	// Immich got its admin, and Background work its key.
	if !slices.Contains(*calls, "POST /api/auth/admin-sign-up") || host.settings.APIKey != "immich-key" || !host.settings.Enabled || host.settings.Node != "pve" || host.settings.Timezone != "Europe/Brussels" {
		t.Fatalf("calls = %v, settings = %+v", *calls, host.settings)
	}

	// The secrets are gone once the run is done.
	if service.plan != nil {
		t.Fatal("kept the plan after the run")
	}

	// A second run leaves what is there alone.
	if err := service.Start(context.Background(), validPlan()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, service, StateDone)
	if len(host.order) != 3 {
		t.Fatalf("installed again: %v", host.order)
	}
}

func TestSetupPausesAtAFailure(t *testing.T) {
	immich, _ := fakeImmich(t)
	host := &fakeHost{installed: map[string]bool{}, requests: map[string]agent.InstallRequest{}, failApp: agent.JellyfinApp}
	service := New(Options{Apps: host, Agent: host, Backups: fakeBackups{host}, Workloads: fakeWorkloads{host, immich.URL}, Poll: time.Millisecond})
	plan := validPlan()
	plan.Apps = []string{agent.JellyfinApp, agent.MediaStackApp}
	plan.ProxmoxBackups = false
	if err := service.Start(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	view := waitFor(t, service, StatePaused)
	failed := view.Steps[slices.IndexFunc(view.Steps, func(s Step) bool { return s.State == StepFailed })]
	if failed.ID != "app:jellyfin" || failed.Message != "the NAS refused the account" {
		t.Fatalf("failed = %+v", failed)
	}
	// Retry fails again; Skip goes on without Jellyfin.
	service.Retry()
	waitFor(t, service, StatePaused)
	if err := service.Skip(); err != nil {
		t.Fatal(err)
	}
	view = waitFor(t, service, StateDone)
	if view.Steps[1].State != StepSkipped || !host.installed[agent.MediaStackApp] {
		t.Fatalf("steps = %+v", view.Steps)
	}
	// Without Jellyfin, the media stack gets no Jellyfin key or admin.
	if media := host.requests[agent.MediaStackApp].MediaStackAnswers; media.JellyfinAPIKey != "" || media.JellyfinAdminUsername != "" {
		t.Fatalf("media = %+v", media)
	}
}

func TestMediaStackGetsOnlyAJellyfinKeyThatFits(t *testing.T) {
	host := &fakeHost{installed: map[string]bool{agent.JellyfinApp: true}, requests: map[string]agent.InstallRequest{}}
	host.bindings = []agent.Binding{{ID: "videos", Role: "videos", Mounted: true}}
	// The Jellyfin page points at another Jellyfin, with a key of another form.
	service := New(Options{Apps: host, Agent: host, Backups: fakeBackups{host}, Workloads: fakeWorkloads{host, ""},
		JellyfinKey: func() string { return "dev-key" }, Poll: time.Millisecond})
	plan := validPlan()
	plan.Apps, plan.ProxmoxBackups = []string{agent.MediaStackApp}, false
	if err := service.Start(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	waitFor(t, service, StateDone)
	media := host.requests[agent.MediaStackApp].MediaStackAnswers
	if media.JellyfinAPIKey != "" || media.JellyfinAdminUsername != "nick" || media.Validate() != nil {
		t.Fatalf("media = %+v, %v", media, media.Validate())
	}
}

func TestViewShowsGamingAsNotReady(t *testing.T) {
	host := &fakeHost{installed: map[string]bool{agent.JellyfinApp: true}, requests: map[string]agent.InstallRequest{}}
	service := New(Options{Apps: host, Agent: host, Backups: fakeBackups{host}, Workloads: fakeWorkloads{host, ""}})
	view := service.View(context.Background())
	gaming := view.Apps[slices.IndexFunc(view.Apps, func(a AppChoice) bool { return a.ID == agent.GamingApp })]
	if gaming.Ready || len(gaming.Missing) != 1 || view.HostBytes != 32<<30 || !view.Apps[0].Installed || view.State != StateIdle {
		t.Fatalf("view = %+v", view)
	}
}
