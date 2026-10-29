package apps

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
)

// photosAgent is a host agent with the Photos backups.
type photosAgent struct {
	*fakeAgent
	backupMu  sync.Mutex
	view      agent.PhotosBackupView
	reads     int
	recovered []agent.AppAction
	saved     []agent.PhotosBackupSettings
	saveErr   error
}

func (p *photosAgent) PhotosBackups(context.Context) (agent.PhotosBackupView, error) {
	p.backupMu.Lock()
	defer p.backupMu.Unlock()
	p.reads++
	return p.view, nil
}

func (p *photosAgent) SavePhotosBackups(_ context.Context, settings agent.PhotosBackupSettings) error {
	p.backupMu.Lock()
	defer p.backupMu.Unlock()
	if p.saveErr != nil {
		return p.saveErr
	}
	p.saved = append(p.saved, settings)
	p.view.Settings = settings
	return nil
}

func (p *photosAgent) RecoverPhotos(_ context.Context, vmid int, action agent.AppAction) error {
	p.backupMu.Lock()
	p.recovered = append(p.recovered, action)
	p.backupMu.Unlock()
	p.set(agent.AppInstallStatus{App: agent.PhotosApp, Action: action, State: agent.UpgradeRunning})
	return nil
}

func (p *photosAgent) actions() []agent.AppAction {
	p.backupMu.Lock()
	defer p.backupMu.Unlock()
	return append([]agent.AppAction(nil), p.recovered...)
}

func photosService(status string) (*Service, *photosAgent) {
	fake := &photosAgent{fakeAgent: &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}},
		view: agent.PhotosBackupView{Settings: agent.DefaultPhotosBackupSettings()}}
	pve := &fakeProxmox{resources: append(baseResources(), proxmox.Resource{Type: "lxc", Node: "pve", VMID: 170, Name: "photos", Tags: "iglo;photos", Status: status})}
	return New(Options{Agent: fake, Proxmox: pve, SelfVMID: 100, PollInterval: time.Hour}), fake
}

func TestLastPhotosSlot(t *testing.T) {
	zone := time.FixedZone("CET", 3600)
	for _, c := range []struct {
		at   string
		now  time.Time
		want time.Time
	}{
		{"01:00", time.Date(2026, 10, 29, 1, 0, 0, 0, zone), time.Date(2026, 10, 29, 1, 0, 0, 0, zone)},
		{"01:00", time.Date(2026, 10, 29, 0, 59, 0, 0, zone), time.Date(2026, 10, 28, 1, 0, 0, 0, zone)},
		{"23:30", time.Date(2026, 11, 1, 8, 0, 0, 0, zone), time.Date(2026, 10, 31, 23, 30, 0, 0, zone)},
	} {
		if got := lastPhotosSlot(c.at, c.now); !got.Equal(c.want) {
			t.Errorf("%s at %v: got %v, want %v", c.at, c.now, got, c.want)
		}
	}
	if !lastPhotosSlot("1am", time.Now()).IsZero() {
		t.Fatal("an invalid time made a slot")
	}
}

func TestNightlyPhotosBackupStartsOncePerNight(t *testing.T) {
	s, fake := photosService("running")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	free := func(context.Context) string { return "" }
	night := time.Date(2026, 10, 29, 0, 59, 0, 0, time.Local)

	s.photosBackupTick(ctx, night, free)
	s.photosBackupTick(ctx, night.Add(time.Minute), free)
	if got := fake.actions(); len(got) != 1 || got[0] != agent.ActionBackup {
		t.Fatalf("first night: %v", got)
	}
	if s.PhotosBackupRunning(ctx) == "" {
		t.Fatal("heavy work does not wait for the Photos backup")
	}
	fake.set(agent.AppInstallStatus{App: agent.PhotosApp, Action: agent.ActionBackup, State: agent.UpgradeIdle})
	s.photosBackupTick(ctx, night.Add(2*time.Minute), free)
	s.photosBackupTick(ctx, night.Add(5*time.Hour), free)
	if got := fake.actions(); len(got) != 1 {
		t.Fatalf("the backup ran twice in one night: %v", got)
	}
	s.photosBackupTick(ctx, night.Add(24*time.Hour+time.Minute), free)
	if got := fake.actions(); len(got) != 2 {
		t.Fatalf("second night: %v", got)
	}
}

func TestNightlyPhotosBackupWaitsForBackupsAndUpdates(t *testing.T) {
	s, fake := photosService("running")
	var notes []string
	s.Notify = func(_ context.Context, text string) { notes = append(notes, text) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reason := "Backups are running"
	hold := func(context.Context) string { return reason }
	slot := time.Date(2026, 10, 29, 1, 0, 0, 0, time.Local)

	s.photosBackupTick(ctx, slot, hold)
	s.photosBackupTick(ctx, slot.Add(time.Hour), hold)
	if got := fake.actions(); len(got) != 0 {
		t.Fatalf("started while Proxmox backups ran: %v", got)
	}
	reason = ""
	s.photosBackupTick(ctx, slot.Add(2*time.Hour), hold)
	if got := fake.actions(); len(got) != 1 {
		t.Fatalf("did not start after the backups: %v", got)
	}

	// A night that waits too long is skipped, and says why.
	fake.set(agent.AppInstallStatus{State: agent.UpgradeIdle})
	reason = "Updates are running"
	next := slot.Add(24 * time.Hour)
	s.photosBackupTick(ctx, next, hold)
	s.photosBackupTick(ctx, next.Add(photosBackupDelay+time.Minute), hold)
	if got := fake.actions(); len(got) != 1 {
		t.Fatalf("started while updates ran: %v", got)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "did not start") || !strings.Contains(notes[0], "Updates are running") {
		t.Fatalf("notes: %q", notes)
	}
}

func TestNightlyPhotosBackupCountsABackupByHand(t *testing.T) {
	s, fake := photosService("running")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slot := time.Date(2026, 10, 29, 1, 0, 0, 0, time.Local)
	fake.view.Status.Started = slot.Add(time.Minute)
	s.photosBackupTick(ctx, slot.Add(2*time.Minute), func(context.Context) string { return "" })
	if got := fake.actions(); len(got) != 0 {
		t.Fatalf("backed up twice: %v", got)
	}
}

func TestNightlyPhotosBackupNeedsARunningContainer(t *testing.T) {
	s, fake := photosService("stopped")
	var notes []string
	s.Notify = func(_ context.Context, text string) { notes = append(notes, text) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slot := time.Date(2026, 10, 29, 1, 0, 0, 0, time.Local)
	s.photosBackupTick(ctx, slot, func(context.Context) string { return "" })
	s.photosBackupTick(ctx, slot.Add(time.Minute), func(context.Context) string { return "" })
	if len(fake.actions()) != 0 || len(notes) != 1 || !strings.Contains(notes[0], "not running") {
		t.Fatalf("actions %v, notes %q", fake.actions(), notes)
	}
}

func TestPhotosBackupOnTheAppsPage(t *testing.T) {
	s, fake := photosService("running")
	fake.view.Status = agent.PhotosBackupStatus{Result: "ok", Snapshot: "1a2b3c4d", NAS: agent.PhotosRepository{Bytes: 1 << 30, Snapshots: 3}}
	ctx := context.Background()
	for range 3 {
		view, err := s.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, app := range view.Apps {
			if (app.ID == agent.PhotosApp) != (app.PhotosBackup != nil) {
				t.Fatalf("%s: backup %+v", app.ID, app.PhotosBackup)
			}
		}
	}
	if fake.reads != 1 {
		t.Fatalf("the host agent was asked %d times, want once while it is cached", fake.reads)
	}

	settings := agent.DefaultPhotosBackupSettings()
	settings.OffsitePath = "/mnt/usb"
	if err := s.SavePhotosBackups(ctx, settings); err != nil {
		t.Fatal(err)
	}
	view, _ := s.Status(ctx)
	for _, app := range view.Apps {
		if app.ID == agent.PhotosApp && app.PhotosBackup.Settings.OffsitePath != "/mnt/usb" {
			t.Fatalf("saving did not refresh the view: %+v", app.PhotosBackup)
		}
	}
	settings.Time = "1am"
	if err := s.SavePhotosBackups(ctx, settings); !errors.Is(err, agent.ErrInvalidAnswers) {
		t.Fatalf("invalid time: %v", err)
	}
}

func TestPhotosBackupMessage(t *testing.T) {
	s, fake := photosService("running")
	ctx := context.Background()
	fake.view.Status = agent.PhotosBackupStatus{Result: "ok", Snapshot: "1a2b3c4d",
		Offsite: agent.PhotosRepository{Result: "missing", Message: "/mnt/usb cannot be used: its disk is not mounted"}}
	got := s.photosRecoveryMessage(ctx, agent.ActionBackup, agent.AppInstallStatus{State: agent.UpgradeSucceeded})
	if !strings.Contains(got, "1a2b3c4d") || !strings.Contains(got, "No copy off the NAS") {
		t.Fatalf("got %q", got)
	}
	fake.view.Status = agent.PhotosBackupStatus{Result: "failed", Message: "The backup failed while dumping the database"}
	got = s.photosRecoveryMessage(ctx, agent.ActionBackup, agent.AppInstallStatus{State: agent.UpgradeFailed, Message: "the backup stopped with exit code 1"})
	if !strings.Contains(got, "dumping the database") {
		t.Fatalf("got %q", got)
	}
}

// After a restart, the manager has no operation in memory: the host agent says
// what runs, also the backup at the start of a Photos update.
func TestPhotosBackupHoldAfterRestart(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		status agent.AppInstallStatus
		want   string
	}{
		{agent.AppInstallStatus{App: agent.PhotosApp, Action: agent.ActionBackup, State: agent.UpgradeRunning}, "The Photos backup is running"},
		{agent.AppInstallStatus{App: agent.PhotosApp, Action: agent.ActionVerify, State: agent.UpgradeRunning}, "The Photos backups are being checked"},
		{agent.AppInstallStatus{App: agent.PhotosApp, Action: agent.ActionUpdate, State: agent.UpgradeRunning, Log: "==> Dumping the Immich database\n"}, "The Photos backup before its update is running"},
		{agent.AppInstallStatus{App: agent.PhotosApp, Action: agent.ActionUpdate, State: agent.UpgradeRunning, Log: "==> Photos is backed up in snapshot 1a2b3c4d\n==> Pulling\n"}, ""},
		{agent.AppInstallStatus{App: agent.PhotosApp, Action: agent.ActionBackup, State: agent.UpgradeSucceeded}, ""},
		{agent.AppInstallStatus{App: agent.MusicApp, Action: agent.ActionUpdate, State: agent.UpgradeRunning}, ""},
	} {
		s, fake := photosService("running")
		fake.set(c.status)
		if got := s.PhotosBackupRunning(ctx); got != c.want {
			t.Errorf("%s %s %s: got %q, want %q", c.status.App, c.status.Action, c.status.State, got, c.want)
		}
	}
}

func TestPhotosBackupHoldAsksTheAgentRarely(t *testing.T) {
	s, fake := photosService("running")
	counting := &countingStatus{photosAgent: fake}
	s.Agent = counting
	for range 5 {
		s.PhotosBackupRunning(context.Background())
	}
	if counting.calls != 1 {
		t.Fatalf("asked the host agent %d times, want once in 10 seconds", counting.calls)
	}
}

type countingStatus struct {
	*photosAgent
	calls int
}

func (c *countingStatus) AppInstallStatus(ctx context.Context) (agent.AppInstallStatus, error) {
	c.calls++
	return c.photosAgent.AppInstallStatus(ctx)
}
