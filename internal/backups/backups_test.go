package backups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
)

type fakeProxmox struct {
	mu        sync.Mutex
	resources []proxmox.Resource
	jobs      []proxmox.BackupJob
	backups   map[string][]proxmox.Backup
	tasks     []proxmox.Task
	running   []proxmox.Task
	calls     []string
	failTask  string
}

func (f *fakeProxmox) record(call string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return "UPID:" + call
}

func (f *fakeProxmox) Resources(context.Context) ([]proxmox.Resource, error) { return f.resources, nil }
func (f *fakeProxmox) BackupJobs(context.Context) ([]proxmox.BackupJob, error) {
	return f.jobs, nil
}
func (f *fakeProxmox) BackupStorages(context.Context, string) ([]proxmox.BackupStorage, error) {
	return []proxmox.BackupStorage{{Storage: "nas", Shared: true}, {Storage: "local"}}, nil
}
func (f *fakeProxmox) Backups(_ context.Context, _, storage string) ([]proxmox.Backup, error) {
	return f.backups[storage], nil
}
func (f *fakeProxmox) RunningBackupTasks(context.Context, string) ([]proxmox.Task, error) {
	return f.running, nil
}
func (f *fakeProxmox) BackupTasks(context.Context, string, int) ([]proxmox.Task, error) {
	return f.tasks, nil
}
func (f *fakeProxmox) BackupGuest(_ context.Context, _ string, vmid int, storage string) (string, error) {
	return f.record(fmt.Sprintf("backup %d %s", vmid, storage)), nil
}
func (f *fakeProxmox) GuestStorage(context.Context, string, proxmox.GuestType, int) (string, error) {
	return "local-lvm", nil
}
func (f *fakeProxmox) RestoreGuest(_ context.Context, _ string, guestType proxmox.GuestType, vmid int, volid, storage string) (string, error) {
	return f.record(fmt.Sprintf("restore %s %d %s %s", guestType, vmid, volid, storage)), nil
}
func (f *fakeProxmox) DeleteBackup(_ context.Context, _, storage, volid string) error {
	f.record("delete " + volid)
	return nil
}
func (f *fakeProxmox) RunGuestAction(_ context.Context, _ string, _ proxmox.GuestType, vmid int, action proxmox.GuestAction) (string, error) {
	return f.record(fmt.Sprintf("%s %d", action, vmid)), nil
}
func (f *fakeProxmox) WaitTask(_ context.Context, _, upid string) error {
	if f.failTask != "" && strings.Contains(upid, f.failTask) {
		return errors.New("proxmox task failed: " + f.failTask)
	}
	return nil
}

// fakeAgent restores at once, like the host agent's Restorer would, and
// records it in the calls of pve so the order shows.
type fakeAgent struct {
	saved    []agent.BackupJob
	pve      *fakeProxmox
	mu       sync.Mutex
	status   agent.RestoreStatus
	failWith string
}

func (f *fakeAgent) SaveBackupJob(_ context.Context, job agent.BackupJob) error {
	f.saved = append(f.saved, job)
	return nil
}

func (f *fakeAgent) Restore(_ context.Context, request agent.RestoreRequest) (agent.RestoreStatus, error) {
	kind := "host restore"
	if request.Check {
		kind = "host check"
	}
	f.pve.record(fmt.Sprintf("%s %d %s %s", kind, request.VMID, request.Volid, request.Storage))
	f.mu.Lock()
	defer f.mu.Unlock()
	started := time.Now()
	f.status = agent.RestoreStatus{Request: request, Succeeded: f.failWith == "", Message: f.failWith, StartedAt: started, FinishedAt: started}
	return agent.RestoreStatus{Running: true, Request: request, StartedAt: started}, nil
}

func (f *fakeAgent) RestoreStatus(context.Context) (agent.RestoreStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

type harness struct {
	service  *Service
	pve      *fakeProxmox
	agent    *fakeAgent
	mu       sync.Mutex
	messages []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		pve: &fakeProxmox{
			resources: []proxmox.Resource{
				{Type: "node", Node: "pve", Status: "online"},
				{Type: "lxc", Node: "pve", VMID: 100, Name: "iglo", Status: "running"},
				{Type: "lxc", Node: "pve", VMID: 101, Name: "jellyfin", Status: "running"},
				{Type: "qemu", Node: "pve", VMID: 200, Name: "windows", Status: "stopped"},
			},
			backups: map[string][]proxmox.Backup{
				"local": {
					{VolID: "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst", VMID: 101, CTime: 1790000000, Format: "tar.zst"},
					{VolID: "local:backup/vzdump-lxc-101-2026_09_30-03_00_01.tar.zst", VMID: 101, CTime: 1790086400, Format: "tar.zst", Protected: true},
					{VolID: "local:backup/vzdump-qemu-200-2026_09_30-03_00_01.vma.zst", VMID: 200, CTime: 1790086400, Format: "vma.zst"},
				},
			},
		},
	}
	h.agent = &fakeAgent{pve: h.pve}

	service, err := New(Options{
		DataDir:     t.TempDir(),
		Proxmox:     h.pve,
		Agent:       h.agent,
		SelfVMID:    100,
		Logger:      slog.New(slog.DiscardHandler),
		RestorePoll: time.Millisecond,
		Notify: func(_ context.Context, text string) {
			h.mu.Lock()
			h.messages = append(h.messages, text)
			h.mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.service = service

	return h
}

// wait blocks until the background operation is done.
func (h *harness) wait(t *testing.T) Run {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		h.service.mu.Lock()
		busy, history := h.service.busy, h.service.state.History
		h.service.mu.Unlock()
		if busy == "" && len(history) > 0 {
			return history[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("operation did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStatusGroupsBackups(t *testing.T) {
	h := newHarness(t)
	h.pve.jobs = []proxmox.BackupJob{{
		ID: agent.BackupJobID, Schedule: "mon,thu 02:30", Storage: "local", Exclude: "200",
		PruneBackups: json.RawMessage(`{"keep-daily":"7"}`),
	}}

	view, err := h.service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	job := view.Job
	if !job.Exists || !job.Enabled || !slices.Equal(job.Days, []string{"mon", "thu"}) || job.Hour != 2 || job.Minute != 30 || job.KeepDaily != 7 {
		t.Fatalf("job = %+v", job)
	}

	jellyfin, windows := view.Guests[1], view.Guests[2]
	if !jellyfin.Included || windows.Included || !view.Guests[0].Self {
		t.Errorf("included: jellyfin %v, windows %v", jellyfin.Included, windows.Included)
	}
	if len(jellyfin.Backups) != 2 || !jellyfin.Backups[0].Protected {
		t.Errorf("jellyfin backups should be newest first: %+v", jellyfin.Backups)
	}
	if len(view.Storages) != 2 {
		t.Errorf("storages = %+v", view.Storages)
	}
}

func TestDefaultJobUsesLocalStorage(t *testing.T) {
	view, err := newHarness(t).service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if view.Job.Exists || view.Job.Storage != "local" || view.Job.Hour != 3 || view.Guests[1].Included {
		t.Fatalf("job = %+v", view.Job)
	}
}

func TestCustomScheduleIsKept(t *testing.T) {
	h := newHarness(t)
	h.pve.jobs = []proxmox.BackupJob{{ID: agent.BackupJobID, Schedule: "*-*-1 04:00", Storage: "local"}}

	view, _ := h.service.Status(context.Background())
	if view.Job.Custom != "*-*-1 04:00" {
		t.Fatalf("job = %+v", view.Job)
	}
}

func TestRestoreRunningContainer(t *testing.T) {
	h := newHarness(t)
	volid := "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"

	if err := h.service.Restore(context.Background(), 101, volid); err != nil {
		t.Fatal(err)
	}
	run := h.wait(t)

	// A container is restored by the host agent: only root may restore its bind mounts.
	want := []string{"shutdown 101", "host restore 101 " + volid + " local-lvm", "start 101"}
	if !run.Succeeded || !slices.Equal(h.pve.calls, want) {
		t.Fatalf("run = %+v, calls = %v", run, h.pve.calls)
	}
	if len(h.messages) != 1 || !strings.HasPrefix(h.messages[0], "✅ jellyfin (101): restored") {
		t.Fatalf("messages = %v", h.messages)
	}
}

func TestRestoreStopsWhenShutdownFails(t *testing.T) {
	h := newHarness(t)
	h.pve.failTask = "shutdown"

	if err := h.service.Restore(context.Background(), 101, "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"); err != nil {
		t.Fatal(err)
	}
	h.wait(t)

	if len(h.pve.calls) < 2 || h.pve.calls[1] != "stop 101" {
		t.Fatalf("calls = %v", h.pve.calls)
	}
}

func TestRestoreRefusals(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	cases := map[string]struct {
		vmid  int
		volid string
	}{
		"own container":   {100, "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"},
		"other guest":     {200, "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"},
		"unknown backup":  {101, "local:backup/nope.tar.zst"},
		"unknown guest":   {999, "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"},
		"no storage part": {101, "nope"},
	}
	for name, c := range cases {
		if err := h.service.Restore(ctx, c.vmid, c.volid); err == nil {
			t.Errorf("%s: restore was allowed", name)
		}
	}
	if len(h.pve.calls) != 0 {
		t.Fatalf("calls = %v", h.pve.calls)
	}
}

func TestBackUpUsesJobStorage(t *testing.T) {
	h := newHarness(t)
	h.pve.jobs = []proxmox.BackupJob{{ID: agent.BackupJobID, Schedule: "03:00", Storage: "nas"}}

	if err := h.service.BackUp(context.Background(), 101); err != nil {
		t.Fatal(err)
	}
	h.wait(t)

	if !slices.Equal(h.pve.calls, []string{"backup 101 nas"}) {
		t.Fatalf("calls = %v", h.pve.calls)
	}
}

func TestDeleteRefusesProtectedBackup(t *testing.T) {
	h := newHarness(t)

	err := h.service.Delete(context.Background(), "local:backup/vzdump-lxc-101-2026_09_30-03_00_01.tar.zst")
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v", err)
	}

	if err := h.service.Delete(context.Background(), "local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"); err != nil {
		t.Fatal(err)
	}
	h.wait(t)
	if !slices.Equal(h.pve.calls, []string{"delete local:backup/vzdump-lxc-101-2026_09_29-03_00_01.tar.zst"}) {
		t.Fatalf("calls = %v", h.pve.calls)
	}
}

func TestMonitorAlertsForeignFailures(t *testing.T) {
	h := newHarness(t)
	h.service.state.LastTask = 1000
	h.service.ours["UPID:ours"] = true
	h.pve.tasks = []proxmox.Task{
		{UPID: "UPID:old", Status: "job errors", EndTime: 900},
		{UPID: "UPID:ours", Status: "job errors", EndTime: 1100},
		{UPID: "UPID:ok", ID: "101", Status: "OK", EndTime: 1200},
		{UPID: "UPID:warn", Status: "WARNINGS: 1", EndTime: 1250},
		{UPID: "UPID:failed", Status: "job errors", EndTime: 1300},
	}

	h.service.checkTasks(context.Background())
	if len(h.messages) != 1 || h.messages[0] != "❌ Backup of all guests on pve failed: job errors" {
		t.Fatalf("messages = %v", h.messages)
	}
	if h.service.state.LastTask != 1300 {
		t.Fatalf("last task = %d", h.service.state.LastTask)
	}
	// The history shows the backups of the job, newest first, so the overview
	// sees a failed night. A backup of Iglo-OS is in it already.
	history := h.service.state.History
	if len(history) != 3 || !history[0].Scheduled || history[0].Succeeded || history[0].Target != "All guests" || history[0].Message != "Scheduled backup: job errors" {
		t.Fatalf("history = %+v", history)
	}
	if !history[1].Succeeded || history[2].Target != "jellyfin (101)" || history[2].VMID != 101 || history[2].Message != "Scheduled backup finished" {
		t.Fatalf("history = %+v", history)
	}

	h.service.checkTasks(context.Background())
	if len(h.messages) != 1 || len(h.service.state.History) != 3 {
		t.Fatalf("alerted or added twice: %v, %d runs", h.messages, len(h.service.state.History))
	}
}

func TestSaveJobValidates(t *testing.T) {
	h := newHarness(t)

	if err := h.service.SaveJob(context.Background(), agent.BackupJob{Storage: "bad storage!"}); !errors.Is(err, agent.ErrInvalidBackupJob) {
		t.Fatalf("err = %v", err)
	}
	if len(h.agent.saved) != 0 {
		t.Fatal("invalid job reached the agent")
	}
}

func TestCheckRestoresACopy(t *testing.T) {
	h := newHarness(t)
	volid := "local:backup/vzdump-lxc-101-2026_09_30-03_00_01.tar.zst"
	if err := h.service.Check(context.Background(), volid); err != nil {
		t.Fatal(err)
	}
	run := h.wait(t)
	if !run.Succeeded || run.Kind != RunCheck || !slices.Equal(h.pve.calls, []string{"host check 101 " + volid + " local-lvm"}) {
		t.Fatalf("run = %+v, calls = %v", run, h.pve.calls)
	}
	view, _ := h.service.Status(context.Background())
	if view.Check.Last == nil || view.Check.Last.ID != run.ID || !view.Check.Next.After(time.Now()) || view.Check.Next.After(time.Now().Add(48*time.Hour)) {
		t.Fatalf("check view = %+v", view.Check)
	}

	// A failed check says why, and the original is never stopped or touched.
	h = newHarness(t)
	h.agent.failWith = "the copy started, but its system is \"starting\""
	h.service.Check(context.Background(), volid)
	if run := h.wait(t); run.Succeeded || !strings.Contains(run.Message, "starting") || len(h.pve.calls) != 1 {
		t.Fatalf("run = %+v, calls = %v", run, h.pve.calls)
	}

	if err := h.service.Check(context.Background(), "local:backup/vzdump-qemu-200-2026_09_30-03_00_01.vma.zst"); !errors.Is(err, ErrRefused) {
		t.Fatalf("a VM backup: %v", err)
	}
}

func TestNextCheckGoesRound(t *testing.T) {
	backups := func(volids ...string) []BackupView {
		var views []BackupView
		for index, volid := range volids {
			views = append(views, BackupView{VolID: volid, CreatedAt: time.Unix(int64(index), 0)})
		}
		return views
	}
	guests := []GuestView{
		{VMID: 130, Type: "lxc", Backups: backups("b130-old", "b130-new")},
		{VMID: 101, Type: "lxc", Backups: backups("b101")},
		{VMID: 200, Type: "qemu", Backups: backups("b200")},
		{VMID: 140, Type: "lxc"},
	}
	for after, want := range map[int]string{0: "b101", 101: "b130-new", 130: "b101", 999: "b101"} {
		if got := nextCheck(guests, after, nil, time.Now()); got != want {
			t.Errorf("after %d: %q, want %q", after, got, want)
		}
	}
	if nextCheck(nil, 0, nil, time.Now()) != "" {
		t.Error("a check without backups")
	}
}

func TestMonthlyCheckWaits(t *testing.T) {
	h := newHarness(t)
	now := time.Date(2026, 11, 1, 5, 10, 0, 0, time.Local)
	h.service.Now = func() time.Time { return now }
	holding := true
	h.service.Hold = func() bool { return holding }

	// A game is on: wait.
	h.service.monthlyCheck(context.Background())
	if len(h.pve.calls) != 0 {
		t.Fatalf("checked during a game: %v", h.pve.calls)
	}
	// Not 05:00: wait.
	holding = false
	now = now.Add(2 * time.Hour)
	h.service.monthlyCheck(context.Background())
	if len(h.pve.calls) != 0 {
		t.Fatalf("checked at %s: %v", now, h.pve.calls)
	}

	now = now.Add(22 * time.Hour)
	h.service.monthlyCheck(context.Background())
	h.wait(t)
	if !slices.Equal(h.pve.calls, []string{"host check 101 local:backup/vzdump-lxc-101-2026_09_30-03_00_01.tar.zst local-lvm"}) {
		t.Fatalf("calls = %v", h.pve.calls)
	}
	// This guest is not due again tomorrow.
	now = now.Add(24 * time.Hour)
	h.service.monthlyCheck(context.Background())
	if len(h.pve.calls) != 1 {
		t.Fatalf("checked again after a day: %v", h.pve.calls)
	}
}

func TestCheckCoverageIsPerGuest(t *testing.T) {
	now := time.Now()
	guests := []GuestView{
		{VMID: 101, Type: "lxc", Backups: []BackupView{{VolID: "one"}}},
		{VMID: 102, Type: "lxc", Backups: []BackupView{{VolID: "two"}}},
	}
	checked := map[int]time.Time{101: now}
	if got := nextCheck(guests, 101, checked, now.Add(24*time.Hour)); got != "two" {
		t.Fatal(got)
	}
	checked[102] = now.Add(24 * time.Hour)
	if got := nextCheck(guests, 102, checked, now.Add(2*24*time.Hour)); got != "" {
		t.Fatal(got)
	}
	if got := nextCheck(guests, 102, checked, now.Add(checkEvery)); got != "one" {
		t.Fatal(got)
	}
}

func TestLiveBackupsOnlyPublishChanges(t *testing.T) {
	h := newHarness(t)
	topics := []string{}
	h.service.Changed = func(topic string) { topics = append(topics, topic) }
	h.service.publishChanged()
	h.service.publishChanged()
	if len(topics) != 1 || topics[0] != "backups" {
		t.Fatal(topics)
	}
	h.service.mu.Lock()
	h.service.busy = "Checking"
	h.service.mu.Unlock()
	h.service.publishChanged()
	if len(topics) != 2 {
		t.Fatal("busy state did not publish")
	}
}

func TestRunningSeesOwnRunsAndTheNightlyJob(t *testing.T) {
	h := newHarness(t)
	if h.service.Running(context.Background()) {
		t.Fatal("running with nothing going on")
	}
	// The nightly job runs in Proxmox, not in Iglo-OS.
	h.pve.running = []proxmox.Task{{UPID: "UPID:pve:vzdump", Type: "vzdump", Status: "RUNNING"}}
	if !h.service.Running(context.Background()) {
		t.Fatal("missed the running vzdump task")
	}
	h.pve.running = nil
	// A run of Iglo-OS itself, like a restore check.
	h.service.mu.Lock()
	h.service.busy = "check"
	h.service.mu.Unlock()
	if !h.service.Running(context.Background()) {
		t.Fatal("missed its own run")
	}
}
