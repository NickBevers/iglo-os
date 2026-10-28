package setup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"sync"
	"time"

	"iglo/internal/agent"
	"iglo/internal/apps"
	"iglo/internal/backups"
	"iglo/internal/workloads"
)

// ErrBusy means a setup already runs.
var ErrBusy = errors.New("the setup is already running")

// jellyfinKey is the form of an API key of Jellyfin.
var jellyfinKey = regexp.MustCompile(`^[a-f0-9]{32}$`)

type AppsService interface {
	Status(ctx context.Context) (apps.View, error)
	Install(ctx context.Context, background context.Context, id string, request agent.InstallRequest) error
}

type Agent interface {
	StorageBindings(ctx context.Context) (agent.BindingView, error)
	CreateBinding(ctx context.Context, in agent.BindingInput) error
	UseBindingForBackups(ctx context.Context, id string, on bool) error
	HostMemory(ctx context.Context) (agent.HostMemory, error)
	GamingReady(ctx context.Context) (agent.GamingReadiness, error)
}

type BackupsService interface {
	Status(ctx context.Context) (backups.View, error)
	SaveJob(ctx context.Context, job agent.BackupJob) error
}

type WorkloadsService interface {
	Status() workloads.View
	SaveSettings(workloads.Settings) error
}

type Options struct {
	Apps      AppsService
	Agent     Agent
	Backups   BackupsService
	Workloads WorkloadsService
	// JellyfinKey is the API key that the Jellyfin install made for Iglo-OS.
	JellyfinKey func() string
	// Changed tells the open pages that the setup moved on.
	Changed func()
	// Background is the context of the run, which outlives the request.
	Background context.Context
	// Poll is how often a step looks at a running install. Tests shorten it.
	Poll time.Duration
	// HTTP calls Immich. Tests replace it.
	HTTP *http.Client
	// DataDir keeps the plan of the last setup, without its secrets.
	DataDir string
	Logger  *slog.Logger
}

type StepState string

const (
	StepWaiting StepState = "waiting"
	StepRunning StepState = "running"
	StepDone    StepState = "done"
	StepFailed  StepState = "failed"
	StepSkipped StepState = "skipped"
)

// Step is one thing that the setup does.
type Step struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	State   StepState `json:"state"`
	Message string    `json:"message,omitempty"`
	// App is the app that the step installs, so the page can show its log.
	App string `json:"app,omitempty"`
	run func(ctx context.Context) (string, error)
}

const (
	StateIdle    = "idle"
	StateRunning = "running"
	StatePaused  = "paused"
	StateDone    = "done"
)

type Service struct {
	Options

	mu    sync.Mutex
	state string
	steps []*Step
	// plan holds the secrets while a run lasts, and only in memory.
	plan *Plan
}

func New(options Options) *Service {
	if options.Changed == nil {
		options.Changed = func() {}
	}
	if options.Background == nil {
		options.Background = context.Background()
	}
	if options.Poll == 0 {
		options.Poll = 3 * time.Second
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Service{Options: options, state: StateIdle}
}

func (s *Service) http() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Start checks the plan and runs it in the background.
func (s *Service) Start(ctx context.Context, plan Plan) error {
	covered, err := s.covered(ctx)
	if err != nil {
		return err
	}
	if err := plan.Validate(covered); err != nil {
		return err
	}
	if err := s.savePlan(plan); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateRunning {
		return ErrBusy
	}
	s.plan = &plan
	s.steps = s.stepsOf(plan)
	s.state = StateRunning
	go s.run()
	s.Changed()
	return nil
}

// Retry runs the failed step again and goes on.
func (s *Service) Retry() error {
	return s.resume(func(step *Step) { step.State, step.Message = StepWaiting, "" })
}

// Skip leaves the failed step out and goes on.
func (s *Service) Skip() error {
	return s.resume(func(step *Step) { step.State, step.Message = StepSkipped, "Skipped" })
}

func (s *Service) resume(change func(*Step)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StatePaused || s.plan == nil {
		return errors.New("there is no paused setup")
	}
	for _, step := range s.steps {
		if step.State == StepFailed {
			change(step)
		}
	}
	s.state = StateRunning
	go s.run()
	s.Changed()
	return nil
}

// Cancel ends a paused setup. What it installed stays.
func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateRunning {
		return
	}
	s.state, s.steps, s.plan = StateIdle, nil, nil
	s.Changed()
}

// run does the waiting steps one by one, and pauses at a failure.
func (s *Service) run() {
	for {
		s.mu.Lock()
		index := slices.IndexFunc(s.steps, func(step *Step) bool { return step.State == StepWaiting })
		if index < 0 {
			// Done: the secrets go.
			s.state, s.plan = StateDone, nil
			s.mu.Unlock()
			s.Changed()
			return
		}
		step := s.steps[index]
		step.State = StepRunning
		s.mu.Unlock()
		s.Changed()

		ctx, cancel := context.WithTimeout(s.Background, time.Hour)
		message, err := step.run(ctx)
		cancel()

		s.mu.Lock()
		step.Message = message
		if err != nil {
			step.State, step.Message = StepFailed, err.Error()
			s.state = StatePaused
			s.mu.Unlock()
			s.Logger.Warn("setup step failed", "step", step.ID, "error", err)
			s.Changed()
			return
		}
		step.State = StepDone
		s.mu.Unlock()
		s.Changed()
	}
}

// stepsOf turns the plan into steps, in the order that the apps need them.
func (s *Service) stepsOf(plan Plan) []*Step {
	steps := []*Step{}
	add := func(id, title, app string, run func(ctx context.Context) (string, error)) {
		steps = append(steps, &Step{ID: id, Title: title, State: StepWaiting, App: app, run: run})
	}
	for _, role := range plan.roles() {
		add("share:"+role, fmt.Sprintf("Connect the %s share", shareNames[role]), "", func(ctx context.Context) (string, error) {
			return s.connectShare(ctx, plan, role)
		})
	}
	if plan.ProxmoxBackups {
		add("backups", "Keep Proxmox backups on the NAS", "", func(ctx context.Context) (string, error) {
			return s.backupsOnNAS(ctx)
		})
	}
	for _, app := range Apps {
		if !plan.wants(app) {
			continue
		}
		add("app:"+app, "Install "+appNames[app], app, func(ctx context.Context) (string, error) {
			return s.installApp(ctx, plan, app)
		})
		if app == agent.PhotosApp {
			add("immich", "Create the Immich admin and connect Background work", "", func(ctx context.Context) (string, error) {
				return s.connectImmich(ctx, plan)
			})
		}
	}
	return steps
}

var shareNames = map[string]string{"videos": "Videos", "photos": "Photos", "backups": "Backups", "music": "Music", "inbox": "Inbox", "games": "Games"}

var appNames = map[string]string{
	agent.JellyfinApp: "Jellyfin", agent.MediaStackApp: "the media stack", agent.PhotosApp: "Photos",
	agent.MusicApp: "Music", agent.InboxApp: "Inbox", agent.GamingApp: "Gaming",
}

// covered are the roles of the shares that are connected already.
func (s *Service) covered(ctx context.Context) (map[string]bool, error) {
	view, err := s.Agent.StorageBindings(ctx)
	if err != nil {
		return nil, err
	}
	covered := map[string]bool{}
	for _, binding := range view.Bindings {
		if binding.Mounted && (readOnlyRoles[binding.Role] || !binding.ReadOnly) {
			covered[binding.Role] = true
		}
	}
	return covered, nil
}

// binding finds a mounted share of role that the apps can use, with write
// access unless the apps only read it.
func (s *Service) binding(ctx context.Context, role string) (agent.Binding, error) {
	view, err := s.Agent.StorageBindings(ctx)
	if err != nil {
		return agent.Binding{}, err
	}
	for _, binding := range view.Bindings {
		if binding.Role == role && binding.Mounted && (readOnlyRoles[role] || !binding.ReadOnly) {
			return binding, nil
		}
	}
	return agent.Binding{}, fmt.Errorf("no %s share is connected", shareNames[role])
}

// connectShare connects the share of role, or uses one that is connected.
func (s *Service) connectShare(ctx context.Context, plan Plan, role string) (string, error) {
	if binding, err := s.binding(ctx, role); err == nil {
		return "Uses the share " + binding.ID, nil
	}
	view, err := s.Agent.StorageBindings(ctx)
	if err != nil {
		return "", err
	}
	// The ID is the role, or the role with a number when that ID is taken.
	id := role
	for n := 2; slices.ContainsFunc(view.Bindings, func(b agent.Binding) bool { return b.ID == id }); n++ {
		id = fmt.Sprintf("%s%d", role, n)
	}
	if err := s.Agent.CreateBinding(ctx, plan.bindingInput(id, role)); err != nil {
		return "", err
	}
	return "Connected as " + id, nil
}

// backupsOnNAS turns the Backups share into Proxmox backup storage and points
// the backup job at it, keeping a schedule that was set before.
func (s *Service) backupsOnNAS(ctx context.Context) (string, error) {
	binding, err := s.binding(ctx, "backups")
	if err != nil {
		return "", err
	}
	if err := s.Agent.UseBindingForBackups(ctx, binding.ID, true); err != nil {
		return "", err
	}
	storage := agent.ProxmoxBackupStorage(binding.ID)
	view, err := s.Backups.Status(ctx)
	if err != nil {
		return "", err
	}
	if view.Job.Custom != "" {
		return "The schedule was changed in Proxmox. Choose " + storage + " on the Backups page.", nil
	}
	job := view.Job.BackupJob
	if !view.Job.Exists {
		job = agent.BackupJob{Enabled: true, Days: []string{}, Hour: 3, Exclude: []int{}, KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 3}
	}
	job.Storage = storage
	if err := s.Backups.SaveJob(ctx, job); err != nil {
		return "", err
	}
	return "Backups go to " + storage, nil
}

// installApp installs app, or leaves an installed one alone, and waits until
// its install is done.
func (s *Service) installApp(ctx context.Context, plan Plan, app string) (string, error) {
	if view, ok := s.appView(ctx, app); ok && view.Installed {
		return "Already installed", nil
	}
	var request agent.InstallRequest
	switch app {
	case agent.JellyfinApp:
		videos, err := s.binding(ctx, "videos")
		if err != nil {
			return "", err
		}
		request.Jellyfin = plan.jellyfinAnswers(videos.ID)
	case agent.MediaStackApp:
		videos, err := s.binding(ctx, "videos")
		if err != nil {
			return "", err
		}
		jellyfin, _ := s.appView(ctx, agent.JellyfinApp)
		key := ""
		if s.JellyfinKey != nil {
			key = s.JellyfinKey()
		}
		// Only a key of the Jellyfin that the setup made works. The libraries
		// exist already, so without it the stack only skips adding them.
		if !jellyfinKey.MatchString(key) {
			key = ""
		}
		request.MediaStackAnswers = plan.mediaAnswers(videos.ID, key, jellyfin.Installed)
	default:
		bindings := map[string]string{}
		for _, role := range shareRoles[app] {
			binding, err := s.binding(ctx, role)
			if err != nil {
				return "", err
			}
			bindings[role] = binding.ID
		}
		request.Scoped = plan.scopedAnswers(app, bindings)
	}
	if err := s.Apps.Install(ctx, s.Background, app, request); err != nil {
		return "", err
	}
	return s.waitForApp(ctx, app)
}

func (s *Service) appView(ctx context.Context, app string) (apps.AppView, bool) {
	view, err := s.Apps.Status(ctx)
	if err != nil {
		return apps.AppView{}, false
	}
	index := slices.IndexFunc(view.Apps, func(a apps.AppView) bool { return a.ID == app })
	if index < 0 {
		return apps.AppView{}, false
	}
	return view.Apps[index], true
}

// waitForApp waits until the install of app is done, and returns its result.
func (s *Service) waitForApp(ctx context.Context, app string) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(s.Poll):
		}
		view, ok := s.appView(ctx, app)
		switch {
		case !ok:
			continue
		case view.Operation != nil && view.Operation.State == agent.UpgradeFailed:
			return "", errors.New(view.Operation.Message)
		case view.Operation == nil && view.Installed:
			return "Installed", nil
		}
	}
}

// connectImmich creates the admin of the new Immich and connects Background
// work with a key that may only read and pause the queues.
func (s *Service) connectImmich(ctx context.Context, plan Plan) (string, error) {
	current := s.Workloads.Status().Settings
	if current.URL == "" {
		return "", errors.New("Photos has no address yet")
	}
	key, err := s.immichKey(ctx, current.URL, plan.Admin, plan.AllowRestrictedFaceModels)
	if errors.Is(err, errImmichHasAdmin) {
		return "Immich already had an admin. Connect its API key on the Background work page.", nil
	}
	if err != nil {
		return "", err
	}
	memory, err := s.Agent.HostMemory(ctx)
	if err != nil {
		return "", err
	}
	settings := workloads.DefaultSettings()
	settings.Enabled, settings.Workers = true, true
	settings.URL, settings.APIKey, settings.Node = current.URL, key, memory.Node
	settings.Window, settings.Timezone = true, plan.Timezone
	if !plan.AllowRestrictedFaceModels {
		settings.Queues = slices.DeleteFunc(settings.Queues, func(name string) bool { return name == "faceDetection" || name == "facialRecognition" })
	}
	if err := s.Workloads.SaveSettings(settings); err != nil {
		return "", err
	}
	return "Log in to Immich as " + plan.Admin.Email + ". Background work runs heavy jobs from 02:00 to 07:00.", nil
}
