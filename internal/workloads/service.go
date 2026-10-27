// Package workloads defers optional Immich work without stopping its API or database.
package workloads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"iglo/internal/events"
	"iglo/internal/proxmox"
)

var ErrInvalid = errors.New("invalid background-work settings")

// Only optional queues may be controlled. Upload metadata, thumbnails, database
// backups and housekeeping deliberately remain under Immich's control.
var HeavyQueues = []string{"videoConversion", "faceDetection", "facialRecognition", "smartSearch", "duplicateDetection", "ocr"}

type Settings struct {
	Workers    bool     `json:"workers"`
	Enabled    bool     `json:"enabled"`
	URL        string   `json:"url"`
	APIKey     string   `json:"apiKey,omitempty"`
	Node       string   `json:"node"`
	ReserveGiB int      `json:"reserveGiB"`
	MaxQueues  int      `json:"maxQueues"`
	Queues     []string `json:"queues"`
	Window     bool     `json:"window"`
	Start      string   `json:"start"`
	End        string   `json:"end"`
	Timezone   string   `json:"timezone"`
}

func DefaultSettings() Settings {
	return Settings{ReserveGiB: 4, MaxQueues: 1, Queues: []string{"videoConversion", "faceDetection", "facialRecognition", "smartSearch"}, Start: "02:00", End: "07:00", Timezone: "UTC"}
}

func (s Settings) Validate() error {
	invalid := func(message string) error { return fmt.Errorf("%w: %s", ErrInvalid, message) }
	if s.Workers && strings.TrimSpace(s.Node) == "" {
		return invalid("select a Proxmox node before controlling workers")
	}
	if s.URL != "" {
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return invalid("use an http or https server URL without credentials, query or fragment")
		}
	}
	if s.Enabled && (s.URL == "" || s.APIKey == "" || strings.TrimSpace(s.Node) == "") {
		return invalid("connect Immich and select its Proxmox node first")
	}
	if s.ReserveGiB < 1 || s.ReserveGiB > 128 || s.MaxQueues < 1 || s.MaxQueues > 4 || len(s.Queues) == 0 || len(s.Queues) > len(HeavyQueues) {
		return invalid("choose 1–128 GiB headroom, 1–4 queues and at least one heavy queue")
	}
	seen := map[string]bool{}
	for _, name := range s.Queues {
		if !slices.Contains(HeavyQueues, name) || seen[name] {
			return invalid("unknown or repeated queue")
		}
		seen[name] = true
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return invalid("choose a valid timezone")
	}
	start, err := clockMinutes(s.Start)
	if err != nil {
		return invalid("start must be HH:MM")
	}
	end, err := clockMinutes(s.End)
	if err != nil {
		return invalid("end must be HH:MM")
	}
	if s.Window && start == end {
		return invalid("start and end must differ; turn the window off for all-day processing")
	}
	return nil
}

func clockMinutes(value string) (int, error) {
	t, err := time.Parse("15:04", value)
	if err != nil || t.Format("15:04") != value {
		return 0, ErrInvalid
	}
	return t.Hour()*60 + t.Minute(), nil
}

func InWindow(settings Settings, now time.Time) bool {
	if !settings.Window {
		return true
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return false
	}
	now = now.In(loc)
	minute := now.Hour()*60 + now.Minute()
	start, _ := clockMinutes(settings.Start)
	end, _ := clockMinutes(settings.End)
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

type Queue struct {
	Name       string `json:"name"`
	IsPaused   bool   `json:"isPaused"`
	Statistics struct {
		Active  int `json:"active"`
		Waiting int `json:"waiting"`
		Paused  int `json:"paused"`
		Failed  int `json:"failed"`
	} `json:"statistics"`
	Owned bool `json:"owned"`
}

type View struct {
	WorkerReason string    `json:"workerReason,omitempty"`
	WorkerError  string    `json:"workerError,omitempty"`
	Settings     Settings  `json:"settings"`
	Configured   bool      `json:"configured"`
	GamingUntil  time.Time `json:"gamingUntil,omitzero"`
	GamingActive bool      `json:"gamingActive"`
	CheckedAt    time.Time `json:"checkedAt,omitzero"`
	// ImmichGuest is the container whose address the Immich URL has, like
	// "photos (170) on pve". It is empty when no container has it.
	ImmichGuest string `json:"immichGuest,omitempty"`
	// NodeWarning says when Immich runs on another node than the one whose
	// memory the checks read.
	NodeWarning string  `json:"nodeWarning,omitempty"`
	Reason      string  `json:"reason"`
	Error       string  `json:"error,omitempty"`
	FreeBytes   int64   `json:"freeBytes"`
	TotalBytes  int64   `json:"totalBytes"`
	Queues      []Queue `json:"queues"`
}

type diskState struct {
	Settings    Settings  `json:"settings"`
	GamingUntil time.Time `json:"gamingUntil,omitzero"`
	// Owned records pause intent before the request. This lets the manager
	// recover even if it exits after Immich accepts a pause but before replying.
	Owned map[string]bool `json:"owned"`
}

type Resources interface {
	Resources(context.Context) ([]proxmox.Resource, error)
}

type Options struct {
	WorkerAdmission func(context.Context, string, int64) error
	// Changed tells the open pages that a topic changed, like events.Workloads.
	Changed func(topic string)
	DataDir string
	Proxmox Resources
	HTTP    *http.Client
	Now     func() time.Time
}

type Service struct {
	workers          func(context.Context, string, int64) error
	workerMemoryHeld bool
	op               sync.Mutex
	mu               sync.Mutex
	state            diskState // protected by op
	view             View      // protected by mu; no network calls under mu
	path             string
	pve              Resources
	http             *http.Client
	now              func() time.Time
	wake             chan struct{}
	changed          func(topic string)
	shown            string      // mu; what the pages last saw, to tell them of a change
	immich           immichGuest // op
	memoryHeld       bool        // op; avoid rapid pause/resume around the threshold
	busy             bool        // op; work was admitted, so watch host memory closely
}

func New(opts Options) (*Service, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	client := &http.Client{Timeout: 5 * time.Second}
	if opts.HTTP != nil {
		*client = *opts.HTTP
	}
	// Never forward an API key to a redirect target, including same-host HTTP downgrades.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if opts.Changed == nil {
		opts.Changed = func(string) {}
	}
	s := &Service{path: filepath.Join(opts.DataDir, "workloads.json"), pve: opts.Proxmox, http: client, now: opts.Now, wake: make(chan struct{}, 1), workers: opts.WorkerAdmission, changed: opts.Changed}
	s.state = diskState{Settings: DefaultSettings(), Owned: map[string]bool{}}
	data, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &s.state); err != nil {
			return nil, err
		}
		if err := s.state.Settings.Validate(); err != nil {
			return nil, err
		}
	}
	if s.state.Owned == nil {
		s.state.Owned = map[string]bool{}
	}
	for name := range s.state.Owned {
		if !slices.Contains(HeavyQueues, name) {
			return nil, errors.New("unknown owned workload queue")
		}
	}
	s.publish(View{Reason: "Not connected", Queues: []Queue{}})
	return s, nil
}

func (s *Service) Status() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	view := s.view
	view.GamingActive = view.GamingUntil.After(s.now())
	view.Settings.Queues = slices.Clone(view.Settings.Queues)
	view.Queues = slices.Clone(view.Queues)
	return view
}

func (s *Service) publish(view View) {
	view.Settings = s.state.Settings
	view.Settings.Queues = slices.Clone(view.Settings.Queues)
	view.Configured = view.Settings.URL != "" && view.Settings.APIKey != ""
	view.Settings.APIKey = ""
	view.GamingUntil = s.state.GamingUntil
	// Free memory and the time of the check change every time, so they don't count.
	shown := fmt.Sprint(view.Settings, view.Configured, view.GamingUntil, view.Reason, view.Error, view.WorkerReason, view.WorkerError, view.ImmichGuest, view.NodeWarning, view.Queues)
	s.mu.Lock()
	changed, gaming := shown != s.shown, !view.GamingUntil.Equal(s.view.GamingUntil)
	s.view = view
	previous := s.shown
	s.shown = shown
	s.mu.Unlock()
	if changed && previous != "" {
		s.changed(events.Workloads)
		// The Gaming page shows the gaming hold too.
		if gaming {
			s.changed(events.Gaming)
		}
	}
}

func (s *Service) save() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Service) SaveSettings(input Settings) error {
	s.op.Lock()
	defer s.op.Unlock()
	input.URL = strings.TrimRight(strings.TrimSpace(input.URL), "/")
	input.Node = strings.TrimSpace(input.Node)
	old := s.state.Settings
	keyProvided := input.APIKey != ""
	if input.APIKey == "" {
		input.APIKey = old.APIKey
	}
	if err := input.Validate(); err != nil {
		return err
	}
	if input.URL != old.URL && !keyProvided && old.APIKey != "" {
		return fmt.Errorf("%w: enter the API key again when changing the server", ErrInvalid)
	}
	if input.URL != old.URL && len(s.state.Owned) > 0 {
		return fmt.Errorf("%w: disable control and wait for owned queues to resume before changing servers", ErrInvalid)
	}
	input.Queues = slices.Clone(input.Queues)
	s.state.Settings = input
	if err := s.save(); err != nil {
		s.state.Settings = old
		return err
	}
	view := s.Status()
	view.Reason, view.Error = "Applying settings", ""
	s.publish(view)
	s.signal()
	return nil
}

// Discover fills an empty connection after an installation. First-login setup
// and creation of a queue API key still happen in Immich. Existing connections
// and their pause ownership are never replaced by discovery.
func (s *Service) Discover(address string) error {
	s.op.Lock()
	defer s.op.Unlock()
	if s.state.Settings.URL != "" || s.state.Settings.APIKey != "" || len(s.state.Owned) > 0 {
		return nil
	}
	old := s.state.Settings
	s.state.Settings.URL = address
	s.state.Settings.Enabled = false
	if err := s.state.Settings.Validate(); err != nil {
		s.state.Settings = old
		return err
	}
	if err := s.save(); err != nil {
		s.state.Settings = old
		return err
	}
	view := s.Status()
	view.Reason = "Create an Immich admin account and queue API key to connect control"
	s.publish(view)
	s.signal()
	return nil
}

// Gaming protection expires automatically, including across manager restarts.
func (s *Service) Protect(minutes int) error {
	if minutes != 0 && (minutes < 15 || minutes > 480) {
		return fmt.Errorf("%w: choose 15–480 minutes, or 0 to end protection", ErrInvalid)
	}
	s.op.Lock()
	defer s.op.Unlock()
	old := s.state.GamingUntil
	s.state.GamingUntil = time.Time{}
	if minutes > 0 {
		s.state.GamingUntil = s.now().Add(time.Duration(minutes) * time.Minute)
	}
	if err := s.save(); err != nil {
		s.state.GamingUntil = old
		return err
	}
	s.publish(s.Status())
	s.signal()
	return nil
}

const (
	// sessionHold is how long gaming protection lasts after the last check
	// that saw a Moonlight session, so a missed check doesn't end it.
	sessionHold = 15 * time.Minute
	// sessionRefresh is how much of sessionHold may be left before a check
	// moves it on, so the file isn't written on every check.
	sessionRefresh = 10 * time.Minute
)

// HoldWhilePlaying keeps gaming protection at least sessionHold ahead while a
// Moonlight session runs. It never shortens a longer hold, like the one of Play.
func (s *Service) HoldWhilePlaying() error {
	s.op.Lock()
	defer s.op.Unlock()
	now := s.now()
	old := s.state.GamingUntil
	if old.After(now.Add(sessionRefresh)) {
		return nil
	}
	s.state.GamingUntil = now.Add(sessionHold)
	if err := s.save(); err != nil {
		s.state.GamingUntil = old
		return err
	}
	s.publish(s.Status())
	// Only a new hold needs to stop work right away.
	if !old.After(now) {
		s.signal()
	}
	return nil
}

func (s *Service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

const (
	busyInterval = 15 * time.Second
	idleInterval = time.Minute
)

// Run checks often while work is admitted, so memory pressure or a closing
// window stops it quickly, and once a minute while nothing may run. Setting
// changes and gaming protection wake it right away.
func (s *Service) Run(ctx context.Context) {
	for {
		s.Reconcile(ctx)
		s.op.Lock()
		wait := idleInterval
		if s.busy {
			wait = busyInterval
		}
		s.op.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-s.wake:
		}
	}
}

func (s *Service) api(ctx context.Context, method, path string, body io.Reader, out any) error {
	base := s.state.Settings.URL
	// Accept both the Immich root URL and its /api base URL.
	if !strings.HasSuffix(base, "/api") {
		base += "/api"
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return errors.New("invalid Immich request")
	}
	req.Header.Set("x-api-key", s.state.Settings.APIKey)
	req.Header.Set("Content-Type", "application/json")
	response, err := s.http.Do(req)
	if err != nil {
		return errors.New("Immich did not answer; check its URL, connection and certificate")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Immich returned HTTP %d; queue control needs a compatible server and an admin API key with queue read/update access", response.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 128*1024)).Decode(out); err != nil {
			return errors.New("Immich returned invalid queue data")
		}
	}
	return nil
}

// Reconcile pauses admissions, not executing jobs. It never stops containers,
// changes application concurrency settings, or controls GPU allocation directly.
func (s *Service) Reconcile(ctx context.Context) {
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	s.locateImmich(ctx)
	settings := s.state.Settings
	view := View{CheckedAt: s.now(), Reason: "Control disabled", Queues: []Queue{}}
	view.ImmichGuest, view.NodeWarning = s.immich.describe(settings)
	s.busy = false
	s.reconcileWorkers(ctx, &view)
	defer func() {
		for index := range view.Queues {
			view.Queues[index].Owned = s.state.Owned[view.Queues[index].Name]
		}
		s.publish(view)
	}()
	if settings.URL == "" || settings.APIKey == "" {
		view.Reason = "Not connected"
		return
	}
	if err := s.api(ctx, http.MethodGet, "/queues", nil, &view.Queues); err != nil {
		view.Error = err.Error()
		view.Reason = "Status unknown"
		return
	}
	seen := map[string]bool{}
	for _, queue := range view.Queues {
		if seen[queue.Name] || queue.Statistics.Active < 0 || queue.Statistics.Waiting < 0 || queue.Statistics.Paused < 0 {
			view.Error = "Immich returned invalid queue data"
			return
		}
		seen[queue.Name] = true
	}
	blocked := false
	if settings.Enabled {
		view.Reason = "Background processing allowed"
		switch {
		case s.state.GamingUntil.After(s.now()):
			blocked = true
			view.Reason = "Gaming protection"
		case !InWindow(settings, s.now()):
			blocked = true
			view.Reason = "Outside processing window"
		}
		resources, err := s.pve.Resources(ctx)
		found := false
		if err == nil {
			for _, resource := range resources {
				if resource.Type == "node" && resource.Node == settings.Node && resource.Status == "online" && resource.MaxMem > 0 && resource.Mem >= 0 && resource.Mem <= resource.MaxMem {
					found = true
					view.TotalBytes, view.FreeBytes = resource.MaxMem, resource.MaxMem-resource.Mem
				}
			}
		}
		if !found {
			blocked = true
			view.Error = "Host memory is unknown; new heavy work is deferred"
			view.Reason = "Waiting for host memory"
		}
		threshold := int64(settings.ReserveGiB) * (1 << 30)
		if s.memoryHeld {
			threshold += 512 << 20
		}
		if found {
			s.memoryHeld = view.FreeBytes < threshold
		}
		if found && s.memoryHeld {
			blocked = true
			view.Reason = "Preserving RAM headroom"
		}
		for _, name := range settings.Queues {
			if !seen[name] {
				view.Error = "A selected queue is unavailable on this Immich version"
				blocked = true
			}
		}
	}
	// Keep already-active queues in the budget, even if an operator paused them.
	active := map[string]bool{}
	for _, queue := range view.Queues {
		if slices.Contains(settings.Queues, queue.Name) && queue.Statistics.Active > 0 {
			active[queue.Name] = true
		}
	}
	allowed := map[string]bool{}
	if settings.Enabled && !blocked && len(active) > settings.MaxQueues {
		blocked = true
		view.Reason = "Waiting for active queues to drain"
	}
	if settings.Enabled && !blocked {
		for name := range active {
			allowed[name] = true
		}
		for _, name := range settings.Queues {
			if len(allowed) >= settings.MaxQueues {
				break
			}
			for _, queue := range view.Queues {
				if queue.Name == name && (!queue.IsPaused || s.state.Owned[name]) && queue.Statistics.Waiting+queue.Statistics.Paused > 0 {
					allowed[name] = true
				}
			}
		}
	}
	if len(allowed) > 0 {
		s.busy = true
	}
	// Pause first, then resume, so transitions cannot admit two queues at once.
	for _, pause := range []bool{true, false} {
		for index := range view.Queues {
			queue := &view.Queues[index]
			selected := settings.Enabled && slices.Contains(settings.Queues, queue.Name)
			wantedPause := selected && (blocked || !allowed[queue.Name])
			owned := s.state.Owned[queue.Name]
			if wantedPause != pause {
				continue
			}
			if !wantedPause && !owned {
				continue
			} // leave operator-owned pauses alone
			if wantedPause && !queue.IsPaused {
				s.state.Owned[queue.Name] = true
				if err := s.save(); err != nil {
					delete(s.state.Owned, queue.Name)
					view.Error = "Could not save queue ownership; no pause was sent"
					return
				}
				owned = true
			}
			if queue.IsPaused != wantedPause {
				body := strings.NewReader(fmt.Sprintf(`{"isPaused":%t}`, wantedPause))
				if err := s.api(ctx, http.MethodPut, "/queues/"+url.PathEscape(queue.Name), body, nil); err != nil {
					view.Error = err.Error()
					return
				}
				queue.IsPaused = wantedPause
			}
			if !wantedPause && owned {
				delete(s.state.Owned, queue.Name)
				if err := s.save(); err != nil {
					s.state.Owned[queue.Name] = true
					view.Error = "Queue resumed, but ownership could not be saved"
					return
				}
			}
			queue.Owned = s.state.Owned[queue.Name]
		}
	}
	for index := range view.Queues {
		view.Queues[index].Owned = s.state.Owned[view.Queues[index].Name]
	}
	for name := range s.state.Owned {
		if !seen[name] {
			view.Error = "A previously controlled queue is missing; reconnect the original Immich server before changing its address"
		}
	}
}
