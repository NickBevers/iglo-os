// Package updates keeps the Proxmox host and its containers up to date.
// Containers get a snapshot before each update and roll back when it fails.
// The host can't be snapshotted, so it is only checked on the schedule and
// updated when the user clicks.
package updates

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"iglo/internal/agent"
	"iglo/internal/events"
	"iglo/internal/notify"
	"iglo/internal/proxmox"
)

const (
	snapshotPrefix = "iglo_"
	keepSnapshots  = 3
)

var (
	ErrBusy            = errors.New("an update or check is already running")
	ErrInvalidSettings = errors.New("invalid settings")
)

type Proxmox interface {
	Resources(ctx context.Context) ([]proxmox.Resource, error)
	SnapshotSupported(ctx context.Context, node string, vmid int) (bool, error)
	Snapshots(ctx context.Context, node string, vmid int) ([]proxmox.Snapshot, error)
	CreateSnapshot(ctx context.Context, node string, vmid int, name, description string) error
	RollbackSnapshot(ctx context.Context, node string, vmid int, name string) error
	DeleteSnapshot(ctx context.Context, node string, vmid int, name string) error
}

type Agent interface {
	RunJob(ctx context.Context, request agent.JobRequest) (agent.Job, error)
}

type Notifier interface {
	Send(ctx context.Context, text string) error
}

type Options struct {
	Changed func(string)
	DataDir string
	Proxmox Proxmox
	Agent   Agent
	// SelfVMID is the manager's own container. The schedule skips it, and a
	// failed manual update does not roll it back, because that would stop us mid-run.
	SelfVMID int
	Logger   *slog.Logger
	// NewNotifier builds the notifier from the saved ntfy settings. Tests replace it.
	NewNotifier func(Settings) Notifier
	Now         func() time.Time
}

type Service struct {
	Options
	path string

	mu             sync.Mutex
	state          state
	busy           string
	outbox         *notify.Outbox
	customNotifier bool
	shown          string
	notifyWake     chan struct{}
}

func New(options Options) (*Service, error) {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	customNotifier := options.NewNotifier != nil
	if options.NewNotifier == nil {
		options.NewNotifier = func(settings Settings) Notifier {
			return notify.Ntfy{Server: settings.Ntfy.Server, Topic: settings.Ntfy.Topic, Token: settings.Ntfy.Token}
		}
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	path := filepath.Join(options.DataDir, "updates.json")
	loaded, err := loadState(path)
	if err != nil {
		return nil, err
	}

	outbox, err := notify.NewOutbox(filepath.Join(options.DataDir, "notifications.json"))
	if err != nil {
		return nil, err
	}
	return &Service{Options: options, path: path, state: loaded, outbox: outbox, customNotifier: customNotifier, notifyWake: make(chan struct{}, 1)}, nil
}

// start runs an operation in the background, one at a time.
func (s *Service) start(ctx context.Context, name string, operation func(context.Context)) error {
	defer s.publishChanged()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.busy != "" {
		return ErrBusy
	}
	s.busy = name

	go func() {
		defer s.publishChanged()
		defer func() {
			s.mu.Lock()
			s.busy = ""
			s.mu.Unlock()
		}()
		operation(ctx)
	}()

	return nil
}

func (s *Service) StartCheck(ctx context.Context) error {
	return s.start(ctx, "Checking for updates", func(ctx context.Context) { s.checkAll(ctx) })
}

func (s *Service) StartHostUpdate(ctx context.Context) error {
	return s.start(ctx, "Updating the Proxmox host", func(ctx context.Context) {
		run := s.updateHost(ctx)
		s.Notify(ctx, runMessage(run))
	})
}

func (s *Service) StartGuestUpdate(ctx context.Context, vmid int) error {
	return s.start(ctx, fmt.Sprintf("Updating container %d", vmid), func(ctx context.Context) {
		run := s.updateGuest(ctx, vmid, false)
		s.Notify(ctx, runMessage(run))
	})
}

func (s *Service) checkAll(ctx context.Context) Run {
	run := s.newRun(RunCheck, "everything", 0, false)

	hostJob, err := s.Agent.RunJob(ctx, agent.JobRequest{Kind: agent.HostCheck})
	host := targetFromJob(hostJob, err, s.Now())
	host.Name = "Proxmox host"

	guests := map[int]Target{}
	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return s.finishRun(run, RunFailed, "could not list containers: "+err.Error(), "")
	}

	for _, resource := range containers(resources) {
		target := Target{VMID: resource.VMID, Name: resource.Name, Packages: []agent.Package{}, Images: []agent.Image{}}
		if resource.Status == "running" {
			job, err := s.Agent.RunJob(ctx, agent.JobRequest{Kind: agent.GuestCheck, VMID: resource.VMID})
			target = targetFromJob(job, err, s.Now())
			target.VMID = resource.VMID
			target.Name = resource.Name
		} else {
			target.Error = "not running"
		}
		guests[resource.VMID] = target
	}

	s.mu.Lock()
	s.state.Host = host
	s.state.Guests = guests
	s.mu.Unlock()

	withUpdates := 0
	for _, guest := range guests {
		if guest.updateCount() > 0 {
			withUpdates++
		}
	}
	message := fmt.Sprintf("Host: %d updates. Containers with updates: %d of %d.", host.updateCount(), withUpdates, len(guests))

	return s.finishRun(run, RunSucceeded, message, "")
}

func (s *Service) updateHost(ctx context.Context) Run {
	run := s.newRun(RunHostUpdate, "Proxmox host", 0, false)

	job, err := s.Agent.RunJob(ctx, agent.JobRequest{Kind: agent.HostUpgrade})
	if err != nil || job.Status != agent.JobSucceeded {
		return s.finishRun(run, RunFailed, "The update failed. Nothing was rolled back: the host has no snapshots.", jobError(job, err))
	}

	s.mu.Lock()
	s.state.Host.Packages = []agent.Package{}
	s.state.Host.CheckedAt = s.Now()
	s.state.Host.RebootRequired = job.RebootRequired
	s.state.Host.Error = ""
	s.mu.Unlock()

	message := "Updated."
	if job.RebootRequired {
		message = "Updated. A new kernel is installed: reboot the host to use it."
	}

	return s.finishRun(run, RunSucceeded, message, job.Log)
}

func (s *Service) updateGuest(ctx context.Context, vmid int, scheduled bool) Run {
	resource, err := s.findContainer(ctx, vmid)
	name := fmt.Sprintf("container %d", vmid)
	if err == nil {
		name = resource.Name
	}
	run := s.newRun(RunGuestUpdate, name, vmid, scheduled)

	if err != nil {
		return s.finishRun(run, RunFailed, err.Error(), "")
	}
	if resource.Status != "running" {
		return s.finishRun(run, RunFailed, "The container is not running.", "")
	}

	// Scoped apps publish to the NAS. Their database or queue must never be
	// rewound without those files, including through the general Updates page.
	tags := strings.Split(resource.Tags, ";")
	scoped := slices.Contains(tags, "iglo") && slices.ContainsFunc(tags, agent.ScopedApp)
	if !scoped {
		supported, err := s.Proxmox.SnapshotSupported(ctx, resource.Node, vmid)
		if err != nil {
			return s.finishRun(run, RunFailed, "Could not check snapshot support: "+err.Error(), "")
		}
		if supported {
			run.Snapshot = snapshotPrefix + s.Now().Format("20060102_150405")
			if err := s.Proxmox.CreateSnapshot(ctx, resource.Node, vmid, run.Snapshot, "Before update by iglo"); err != nil {
				return s.finishRun(run, RunFailed, "Could not create a snapshot, so nothing was updated: "+err.Error(), "")
			}
		}
	}

	job, err := s.Agent.RunJob(ctx, agent.JobRequest{Kind: agent.GuestUpdate, VMID: vmid})
	failure := jobError(job, err)
	if failure == "" {
		// A container that stopped during the update counts as a failure.
		if after, err := s.findContainer(ctx, vmid); err != nil || after.Status != "running" {
			failure = "The container is not running after the update."
		}
	}

	if failure != "" {
		switch {
		case scoped:
			return s.finishRun(run, RunFailed, "The update failed. Automatic disk rollback is disabled because NAS data is outside the guest. Inspect the operation and application backup before recovering.", logOrError(job, failure))
		case run.Snapshot == "":
			return s.finishRun(run, RunFailed, "The update failed. There is no snapshot, because the storage does not support it.", logOrError(job, failure))
		case vmid == s.SelfVMID:
			return s.finishRun(run, RunFailed, "The update failed. This is the manager itself, so roll back snapshot "+run.Snapshot+" in Proxmox if needed.", logOrError(job, failure))
		}

		if err := s.Proxmox.RollbackSnapshot(ctx, resource.Node, vmid, run.Snapshot); err != nil {
			return s.finishRun(run, RunFailed, "The update failed, and the rollback failed too: "+err.Error(), logOrError(job, failure))
		}
		return s.finishRun(run, RunRolledBack, "The update failed, so the container was rolled back.", logOrError(job, failure))
	}

	if run.Snapshot != "" {
		if err := s.pruneSnapshots(ctx, resource.Node, vmid); err != nil {
			s.Logger.Warn("could not remove old snapshots", "vmid", vmid, "error", err)
		}
	}

	s.mu.Lock()
	target := s.state.Guests[vmid]
	count := target.updateCount()
	target.VMID, target.Name = vmid, resource.Name
	target.Packages, target.Images = []agent.Package{}, []agent.Image{}
	target.CheckedAt, target.Error = s.Now(), ""
	s.state.Guests[vmid] = target
	s.mu.Unlock()

	message := "Updated."
	if count > 0 {
		message = fmt.Sprintf("Updated %d packages and images.", count)
	}

	return s.finishRun(run, RunSucceeded, message, job.Log)
}

// pruneSnapshots keeps the newest few snapshots that we made.
func (s *Service) pruneSnapshots(ctx context.Context, node string, vmid int) error {
	snapshots, err := s.Proxmox.Snapshots(ctx, node, vmid)
	if err != nil {
		return err
	}

	ours := slices.DeleteFunc(snapshots, func(snapshot proxmox.Snapshot) bool {
		return !strings.HasPrefix(snapshot.Name, snapshotPrefix)
	})
	sort.Slice(ours, func(i, j int) bool { return ours[i].SnapTime > ours[j].SnapTime })

	for _, snapshot := range ours[min(keepSnapshots, len(ours)):] {
		if err := s.Proxmox.DeleteSnapshot(ctx, node, vmid, snapshot.Name); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) findContainer(ctx context.Context, vmid int) (proxmox.Resource, error) {
	resources, err := s.Proxmox.Resources(ctx)
	if err != nil {
		return proxmox.Resource{}, err
	}

	for _, resource := range containers(resources) {
		if resource.VMID == vmid {
			return resource, nil
		}
	}

	return proxmox.Resource{}, fmt.Errorf("container %d not found", vmid)
}

func containers(resources []proxmox.Resource) []proxmox.Resource {
	var result []proxmox.Resource
	for _, resource := range resources {
		if resource.Type == "lxc" && resource.Template == 0 {
			result = append(result, resource)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].VMID < result[j].VMID })

	return result
}

func targetFromJob(job agent.Job, err error, now time.Time) Target {
	target := Target{CheckedAt: now, Packages: job.Packages, Images: job.Images, Unsupported: job.Unsupported, RebootRequired: job.RebootRequired}.withLists()
	if message := jobError(job, err); message != "" {
		target.Error = message
	}

	return target
}

func jobError(job agent.Job, err error) string {
	switch {
	case err != nil:
		return err.Error()
	case job.Status != agent.JobSucceeded:
		return job.Error
	}

	return ""
}

func logOrError(job agent.Job, failure string) string {
	if job.Log != "" {
		return job.Log
	}

	return failure
}

func (s *Service) newRun(kind RunKind, target string, vmid int, scheduled bool) Run {
	id := make([]byte, 6)
	rand.Read(id)

	return Run{ID: hex.EncodeToString(id), Kind: kind, Target: target, VMID: vmid, Scheduled: scheduled, StartedAt: s.Now()}
}

func (s *Service) finishRun(run Run, status RunStatus, message, log string) Run {
	defer s.publishChanged()
	run.Status = status
	run.Message = message
	run.FinishedAt = s.Now()
	if len(log) > maxRunLog {
		log = "…" + log[len(log)-maxRunLog:]
	}
	run.Log = log

	s.mu.Lock()
	s.state.History = append([]Run{run}, s.state.History...)
	if len(s.state.History) > maxHistory {
		s.state.History = s.state.History[:maxHistory]
	}
	err := s.state.save(s.path)
	s.mu.Unlock()

	if err != nil {
		s.Logger.Error("could not save update state", "error", err)
	}
	s.Logger.Info("update run finished", "kind", run.Kind, "target", run.Target, "status", run.Status, "message", message)

	return run
}

// Notify persists a short alert before delivery. Failed destinations retry
// independently, including after a manager restart.
func (s *Service) Notify(ctx context.Context, text string) {
	destinations := s.notificationDestinations()
	if err := s.outbox.Enqueue(text, destinations, s.Now()); err != nil {
		s.Logger.Error("could not queue notification", "error", err)
		return
	}
	select {
	case s.notifyWake <- struct{}{}:
	default:
	}
}

func (s *Service) notificationDestinations() []notify.Destination {
	s.mu.Lock()
	settings := s.state.Settings
	s.mu.Unlock()
	if s.customNotifier {
		return []notify.Destination{{ID: "custom", Send: s.NewNotifier(settings).Send}}
	}
	var targets []notify.Destination
	ntfy := notify.Ntfy{Server: settings.Ntfy.Server, Topic: settings.Ntfy.Topic, Token: settings.Ntfy.Token}
	if ntfy.Configured() {
		targets = append(targets, notify.Destination{ID: notify.DestinationID("ntfy", ntfy.Server, ntfy.Topic), Send: ntfy.Send})
	}
	return targets
}

func (s *Service) flushNotifications(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s.outbox.Flush(ctx, s.notificationDestinations(), s.Now()); err != nil {
		s.Logger.Warn("notification delivery pending", "error", err)
	}
}

// RunNotifications wakes on a new alert or the next persisted retry deadline.
// Empty queues have no timer. Startup retries due alerts after a restart.
func (s *Service) RunNotifications(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		s.flushNotifications(ctx)
		var retry <-chan time.Time
		var timer *time.Timer
		if next, ok := s.outbox.NextAttempt(); ok {
			timer = time.NewTimer(max(next.Sub(s.Now()), time.Millisecond))
			retry = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-s.notifyWake:
		case <-retry:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func runMessage(run Run) string {
	icon := map[RunStatus]string{RunSucceeded: "✅", RunFailed: "❌", RunRolledBack: "↩️"}[run.Status]

	return fmt.Sprintf("%s %s: %s", icon, run.Target, run.Message)
}

var (
	// The topic and token rules of ntfy v2.34.0 (server/server.go, user/manager.go).
	ntfyTopicPattern = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)
	ntfyTokenPattern = regexp.MustCompile(`^tk_[A-Za-z0-9]{29}$`)
)

// DefaultNtfyServer is the public server of the ntfy project.
const DefaultNtfyServer = "https://ntfy.sh"

// SettingsInput is what the dashboard sends. An empty ntfy token keeps the
// saved one while the server stays the same.
type SettingsInput struct {
	Schedule Schedule     `json:"schedule"`
	Excluded []int        `json:"excluded"`
	Ntfy     NtfySettings `json:"ntfy"`
}

func (s *Service) SaveSettings(input SettingsInput) error {
	defer s.publishChanged()
	schedule := input.Schedule
	if err := schedule.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ntfy, err := s.ntfySettings(input.Ntfy)
	if err != nil {
		return err
	}

	excluded := input.Excluded
	if excluded == nil {
		excluded = []int{}
	}

	s.state.Settings = Settings{Schedule: schedule, Excluded: excluded, Ntfy: ntfy}

	return s.state.save(s.path)
}

// ntfySettings checks the ntfy part of the input. It needs s.mu.
func (s *Service) ntfySettings(ntfy NtfySettings) (NtfySettings, error) {
	// No topic turns ntfy off.
	if ntfy.Topic == "" {
		return NtfySettings{}, nil
	}
	server, err := NormalizeNtfyServer(ntfy.Server)
	if err != nil {
		return NtfySettings{}, err
	}
	ntfy.Server = server
	// A token belongs to one server.
	if saved := s.state.Settings.Ntfy; ntfy.Token == "" && saved.Server == ntfy.Server {
		ntfy.Token = saved.Token
	}
	if !ntfyTopicPattern.MatchString(ntfy.Topic) {
		return NtfySettings{}, fmt.Errorf("%w: an ntfy topic has 1 to 64 letters, digits, - and _", ErrInvalidSettings)
	}
	if ntfy.Token != "" && !ntfyTokenPattern.MatchString(ntfy.Token) {
		return NtfySettings{}, fmt.Errorf("%w: an ntfy access token starts with tk_ and has 32 characters", ErrInvalidSettings)
	}
	return ntfy, nil
}

// NormalizeNtfyServer checks an ntfy server address and drops the slash at
// the end. Empty is ntfy.sh.
func NormalizeNtfyServer(server string) (string, error) {
	if server == "" {
		return DefaultNtfyServer, nil
	}
	parsed, err := url.Parse(server)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(server) > 200 {
		return "", fmt.Errorf("%w: the ntfy server must be an address like https://ntfy.sh", ErrInvalidSettings)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func ValidNtfyTopic(topic string) bool {
	return ntfyTopicPattern.MatchString(topic)
}

func (s Schedule) Validate() error {
	if s.Weekday < 0 || s.Weekday > 6 || s.Hour < 0 || s.Hour > 23 || s.Minute < 0 || s.Minute > 59 {
		return fmt.Errorf("%w: schedule is out of range", ErrInvalidSettings)
	}

	return nil
}

// Settings returns the saved settings, with the ntfy token.
func (s *Service) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings := s.state.Settings
	settings.Excluded = slices.Clone(settings.Excluded)

	return settings
}

func (s *Service) SendTestNotification(ctx context.Context) error {
	s.mu.Lock()
	settings := s.state.Settings
	s.mu.Unlock()

	if settings.Ntfy.Topic == "" {
		return fmt.Errorf("%w: ntfy is not set up", ErrInvalidSettings)
	}

	return s.NewNotifier(settings).Send(ctx, "👋 Test message from your iglo.")
}

func (s *Service) publishChanged() {
	s.mu.Lock()
	host := s.state.Host
	host.CheckedAt = time.Time{}
	guests := make(map[int]Target, len(s.state.Guests))
	for id, target := range s.state.Guests {
		target.CheckedAt = time.Time{}
		guests[id] = target
	}
	settings := s.state.Settings
	shown := fmt.Sprint(s.busy, host, guests, settings.Schedule, settings.Excluded,
		settings.Ntfy.Server, settings.Ntfy.Topic, settings.Ntfy.Token != "", s.state.History)
	changed := shown != s.shown
	s.shown = shown
	s.mu.Unlock()
	if changed && s.Changed != nil {
		s.Changed(events.Updates)
	}
}
