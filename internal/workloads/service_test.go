package workloads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"iglo/internal/proxmox"
)

type fakeHost struct {
	free int64
	err  error
}

func TestDiscoveryDoesNotReplaceConfiguredServer(t *testing.T) {
	s, err := New(Options{DataDir: t.TempDir(), Proxmox: &fakeHost{free: 8 << 30}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Discover("http://photos:2283"); err != nil {
		t.Fatal(err)
	}
	if s.Status().Settings.URL != "http://photos:2283" || s.Status().Configured || s.Status().Settings.Enabled {
		t.Fatal("discovery enabled an unconfigured controller")
	}
	if err = s.Discover("http://another:2283"); err != nil {
		t.Fatal(err)
	}
	if s.Status().Settings.URL != "http://photos:2283" {
		t.Fatal("overwrote existing connection")
	}
}

func (h *fakeHost) Resources(context.Context) ([]proxmox.Resource, error) {
	return []proxmox.Resource{{Type: "node", Node: "pve", Status: "online", MaxMem: 32 << 30, Mem: (32 << 30) - h.free}}, h.err
}

type fixture struct {
	s           *Service
	host        *fakeHost
	queues      []Queue
	writes      []string
	status      int
	writeStatus int
	key         string
	now         time.Time
	server      *httptest.Server
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{host: &fakeHost{free: 8 << 30}, now: time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC), key: "queue-secret"}
	for _, name := range []string{"videoConversion", "smartSearch", "metadataExtraction", "backupDatabase"} {
		q := Queue{Name: name}
		q.Statistics.Waiting = 3
		f.queues = append(f.queues, q)
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != f.key {
			t.Error("missing API key")
		}
		if f.status != 0 {
			http.Error(w, "sensitive upstream error queue-secret", f.status)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/queues" {
			json.NewEncoder(w).Encode(f.queues)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/api/queues/")
		for i := range f.queues {
			if f.queues[i].Name == name && r.Method == http.MethodPut {
				var body struct {
					IsPaused bool `json:"isPaused"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				f.queues[i].IsPaused = body.IsPaused
				f.writes = append(f.writes, name+":"+map[bool]string{true: "pause", false: "resume"}[body.IsPaused])
				if f.writeStatus != 0 {
					http.Error(w, "reply lost after applying", f.writeStatus)
					return
				}
				json.NewEncoder(w).Encode(f.queues[i])
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.server.Close)
	var err error
	f.s, err = New(Options{DataDir: t.TempDir(), Proxmox: f.host, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings()
	settings.Enabled = true
	settings.URL = f.server.URL
	settings.APIKey = f.key
	settings.Node = "pve"
	settings.Queues = []string{"videoConversion", "smartSearch"}
	if err := f.s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestProtectionAndRecoveryLeaveEssentialAndOperatorQueuesAlone(t *testing.T) {
	f := setup(t)
	f.queues[1].IsPaused = true // the operator paused smart search
	if err := f.s.Protect(120); err != nil {
		t.Fatal(err)
	}
	f.s.Reconcile(context.Background())
	if !slices.Equal(f.writes, []string{"videoConversion:pause"}) {
		t.Fatal(f.writes)
	}
	if f.s.Status().Reason != "Gaming protection" {
		t.Fatal(f.s.Status())
	}
	// Recover ownership from disk, not the old process's in-memory state.
	f.now = f.now.Add(3 * time.Hour)
	restarted, err := New(Options{DataDir: filepath.Dir(f.s.path), Proxmox: f.host, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	restarted.Reconcile(context.Background())
	if !slices.Equal(f.writes, []string{"videoConversion:pause", "videoConversion:resume"}) {
		t.Fatal(f.writes)
	}
	if !f.queues[1].IsPaused || f.queues[2].IsPaused || f.queues[3].IsPaused {
		t.Fatal(f.queues)
	}
}

func TestQueueBudgetAndDrain(t *testing.T) {
	f := setup(t)
	f.s.Reconcile(context.Background())
	if !slices.Equal(f.writes, []string{"smartSearch:pause"}) {
		t.Fatal(f.writes)
	}
	// Two existing active queues cannot be interrupted; stop new admissions
	// until enough work has finished, then admit only one queue.
	f.queues[0].Statistics.Active = 1
	f.queues[1].Statistics.Active = 1
	f.s.Reconcile(context.Background())
	if f.s.Status().Reason != "Waiting for active queues to drain" || !f.queues[0].IsPaused {
		t.Fatal(f.s.Status())
	}
	f.queues[0].Statistics.Active = 0
	f.queues[1].Statistics.Active = 0
	f.queues[0].Statistics.Waiting = 0
	f.s.Reconcile(context.Background())
	if !f.queues[0].IsPaused || f.queues[1].IsPaused {
		t.Fatal(f.queues)
	}
}

func TestMemoryGuardHysteresisAndUnknownHost(t *testing.T) {
	f := setup(t)
	f.host.free = 3 << 30
	f.s.Reconcile(context.Background())
	if f.s.Status().Reason != "Preserving RAM headroom" || len(f.writes) != 2 {
		t.Fatal(f.s.Status(), f.writes)
	}
	f.host.free = (4 << 30) + (256 << 20)
	f.s.Reconcile(context.Background())
	if len(f.writes) != 2 {
		t.Fatal("resumed too close to memory threshold", f.writes)
	}
	f.host.free = 5 << 30
	f.s.Reconcile(context.Background())
	if f.queues[0].IsPaused {
		t.Fatal("did not resume with headroom")
	}
	f.host.err = errors.New("offline")
	f.s.Reconcile(context.Background())
	if f.s.Status().Error == "" || !f.queues[0].IsPaused {
		t.Fatal(f.s.Status())
	}
}

func TestDisableReleasesOwnershipAndServerChangeNeedsKey(t *testing.T) {
	f := setup(t)
	f.s.Protect(120)
	f.s.Reconcile(context.Background())
	settings := f.s.Status().Settings
	settings.URL = "http://different-server:2283"
	if !errors.Is(f.s.SaveSettings(settings), ErrInvalid) {
		t.Fatal("reused a key on a new server")
	}
	settings.APIKey = "new-key"
	if !errors.Is(f.s.SaveSettings(settings), ErrInvalid) {
		t.Fatal("abandoned paused queues")
	}
	settings = f.s.Status().Settings
	settings.Enabled = false
	if err := f.s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	f.s.Reconcile(context.Background())
	if len(f.s.state.Owned) != 0 || f.queues[0].IsPaused || f.queues[1].IsPaused {
		t.Fatal(f.s.state, f.queues)
	}
	settings.URL = f.server.URL + "/api"
	settings.APIKey = f.key
	if err := f.s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	f.s.Reconcile(context.Background())
	if f.s.Status().Error != "" {
		t.Fatal(f.s.Status())
	}
}

func TestFailuresDoNotLeakSecretsOrLosePauseIntent(t *testing.T) {
	f := setup(t)
	f.status = http.StatusUnauthorized
	f.s.Reconcile(context.Background())
	data, _ := json.Marshal(f.s.Status())
	if strings.Contains(string(data), f.key) || strings.Contains(string(data), "sensitive") || len(f.writes) > 0 {
		t.Fatal(string(data))
	}
	if f.s.Status().Error == "" {
		t.Fatal("failed request shown as healthy")
	}
	f.status = 0
	f.s.Protect(120)
	// A pause must not be sent if its recovery intent cannot be persisted.
	f.s.path = filepath.Join(t.TempDir(), "missing", "state.json")
	f.s.Reconcile(context.Background())
	if len(f.writes) != 0 || f.s.Status().Error == "" {
		t.Fatal(f.writes, f.s.Status())
	}
}

func TestSecretFilePermissionsAndViewCopies(t *testing.T) {
	f := setup(t)
	info, err := os.Stat(f.s.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
	view := f.s.Status()
	view.Settings.Queues[0] = "metadataExtraction"
	if f.s.Status().Settings.Queues[0] == "metadataExtraction" {
		t.Fatal("view exposed mutable settings")
	}
}

func TestRecoveryWhenPauseSucceededButReplyFailed(t *testing.T) {
	f := setup(t)
	f.writeStatus = http.StatusBadGateway
	f.s.Protect(120)
	f.s.Reconcile(context.Background())
	if !f.queues[0].IsPaused || !f.s.state.Owned["videoConversion"] || f.s.Status().Error == "" {
		t.Fatal(f.s.Status(), f.s.state)
	}
	restarted, err := New(Options{DataDir: filepath.Dir(f.s.path), Proxmox: f.host, Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	settings := restarted.Status().Settings
	settings.Enabled = false
	if err := restarted.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	f.writeStatus = 0
	restarted.Reconcile(context.Background())
	if f.queues[0].IsPaused || len(restarted.state.Owned) != 0 {
		t.Fatal("abandoned an acknowledged pause", restarted.state)
	}
}

func TestNoCredentialForwardingOnRedirect(t *testing.T) {
	f := setup(t)
	received := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true }))
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer redirect.Close()
	settings := f.s.Status().Settings
	settings.URL = redirect.URL
	settings.APIKey = "fresh-key"
	if err := f.s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	f.s.Reconcile(context.Background())
	if received || f.s.Status().Error == "" {
		t.Fatal("followed credentialed redirect")
	}
}

func TestWindowBoundariesAndValidation(t *testing.T) {
	s := DefaultSettings()
	s.Window = true
	s.Start = "22:00"
	s.End = "06:00"
	s.Timezone = "Europe/Brussels"
	for _, test := range []struct {
		utc     string
		allowed bool
	}{
		{"2026-10-22T19:59:00Z", false}, {"2026-10-22T20:00:00Z", true}, {"2026-10-23T03:59:00Z", true}, {"2026-10-23T04:00:00Z", false},
		{"2026-10-25T01:30:00Z", true}, // daylight saving transition
	} {
		now, _ := time.Parse(time.RFC3339, test.utc)
		if got := InWindow(s, now); got != test.allowed {
			t.Fatalf("%s: %t", test.utc, got)
		}
	}
	for _, change := range []func(*Settings){
		func(s *Settings) { s.Queues = []string{"backupDatabase"} }, func(s *Settings) { s.MaxQueues = 0 }, func(s *Settings) { s.ReserveGiB = 0 },
		func(s *Settings) { s.Start = "25:00" }, func(s *Settings) { s.Start = s.End }, func(s *Settings) { s.Timezone = "invalid" },
		func(s *Settings) { s.URL = "http://user:secret@server" }, func(s *Settings) { s.URL = "http://server?token=secret" },
	} {
		input := s
		change(&input)
		if !errors.Is(input.Validate(), ErrInvalid) {
			t.Fatal(input)
		}
	}
}

func TestHoldWhilePlayingKeepsAShortHoldAhead(t *testing.T) {
	f := setup(t)
	start := f.now
	if err := f.s.HoldWhilePlaying(); err != nil {
		t.Fatal(err)
	}
	if got := f.s.Status().GamingUntil; !got.Equal(start.Add(15 * time.Minute)) {
		t.Fatalf("until = %v", got)
	}
	// A check soon after leaves the hold, so the file isn't written each time.
	f.now = start.Add(4 * time.Minute)
	f.s.HoldWhilePlaying()
	if got := f.s.Status().GamingUntil; !got.Equal(start.Add(15 * time.Minute)) {
		t.Fatalf("until moved early to %v", got)
	}
	f.now = start.Add(6 * time.Minute)
	f.s.HoldWhilePlaying()
	if got := f.s.Status().GamingUntil; !got.Equal(f.now.Add(15 * time.Minute)) {
		t.Fatalf("until = %v", got)
	}
	// The 8 hours of Play stay.
	f.s.Protect(480)
	f.s.HoldWhilePlaying()
	if got := f.s.Status().GamingUntil; !got.Equal(f.now.Add(480 * time.Minute)) {
		t.Fatalf("shortened Play to %v", got)
	}
}
