package apps

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"iglo/internal/agent"
	"iglo/internal/events"
	"iglo/internal/proxmox"
)

// gamingProxmox records guest actions, which only the Gaming app uses.
type gamingProxmox struct {
	fakeProxmox
	actions []proxmox.GuestAction
}

func (f *gamingProxmox) RunGuestAction(_ context.Context, node string, guestType proxmox.GuestType, vmid int, action proxmox.GuestAction) (string, error) {
	if node != "pve" || guestType != proxmox.LXC || vmid != 190 {
		panic("unexpected guest")
	}
	f.actions = append(f.actions, action)
	return "UPID:test", nil
}

func TestGamingStartsAndStopsItsContainer(t *testing.T) {
	pve := &gamingProxmox{}
	gaming := proxmox.Resource{Type: "lxc", Node: "pve", VMID: 190, Name: "gaming", Status: "stopped", Tags: "iglo;gaming"}
	pve.resources = append(baseResources(), gaming)
	service := New(Options{Agent: &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}, Proxmox: pve, SelfVMID: 100})

	view, err := service.Gaming(context.Background())
	if err != nil || !view.Installed || view.VMID != 190 || view.Status != "stopped" {
		t.Fatalf("view = %+v, %v", view, err)
	}
	// The fake agent has no library for Gaming, which the page shows as an error.
	if view.LibraryError == "" {
		t.Error("a missing library must show its reason")
	}

	if err := service.SetGamingPower(context.Background(), false); err != nil || len(pve.actions) != 0 {
		t.Fatalf("stopping a stopped container: %v, %v", err, pve.actions)
	}
	if err := service.SetGamingPower(context.Background(), true); err != nil || len(pve.actions) != 1 || pve.actions[0] != proxmox.Start {
		t.Fatalf("start: %v, %v", err, pve.actions)
	}
	pve.resources[len(pve.resources)-1].Status = "running"
	if err := service.SetGamingPower(context.Background(), false); err != nil || pve.actions[1] != proxmox.Shutdown {
		t.Fatalf("stop: %v, %v", err, pve.actions)
	}
}

func TestGamingWithoutContainer(t *testing.T) {
	service := New(Options{Agent: &fakeAgent{}, Proxmox: &fakeProxmox{resources: baseResources()}, SelfVMID: 100})
	view, err := service.Gaming(context.Background())
	if err != nil || view.Installed || view.Library == nil {
		t.Fatalf("view = %+v, %v", view, err)
	}
	if err := service.SetGamingPower(context.Background(), true); err == nil {
		t.Fatal("started a container that isn't installed")
	}
}

// sessionAgent fakes the Wolf sessions of the gaming container.
type sessionAgent struct {
	fakeAgent
	sessions []agent.GamingSession
	stopped  []string
}

func (f *sessionAgent) GamingSessions(_ context.Context, vmid int) ([]agent.GamingSession, error) {
	if vmid != 190 {
		panic("unexpected guest")
	}
	return slices.Clone(f.sessions), nil
}

func (f *sessionAgent) StopGamingSession(_ context.Context, vmid int, id string) error {
	f.stopped = append(f.stopped, id)
	f.sessions = slices.DeleteFunc(f.sessions, func(session agent.GamingSession) bool { return session.ID == id })
	return nil
}

func TestWatchGamingHoldsWorkAndKeepsOneSession(t *testing.T) {
	pve := &gamingProxmox{}
	pve.resources = append(baseResources(), proxmox.Resource{Type: "lxc", Node: "pve", VMID: 190, Name: "gaming", Status: "running", Tags: "iglo;gaming"})
	fake := &sessionAgent{fakeAgent: fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}}
	service := New(Options{Agent: fake, Proxmox: pve, SelfVMID: 100})
	holds := 0
	hold := func() error { holds++; return nil }

	// No session: no hold.
	service.checkGaming(context.Background(), hold)
	if holds != 0 {
		t.Fatal("held work without a session")
	}

	pc := agent.GamingSession{ID: "1", ClientIP: "192.168.1.50"}
	tv := agent.GamingSession{ID: "2", ClientIP: "192.168.1.60"}
	fake.sessions = []agent.GamingSession{pc}
	service.checkGaming(context.Background(), hold)
	if holds != 1 || len(fake.stopped) != 0 {
		t.Fatalf("holds = %d, stopped = %v", holds, fake.stopped)
	}

	// The PC resumes after the TV joined, so Wolf lists it last. The TV stops.
	fake.sessions = []agent.GamingSession{tv, pc}
	service.checkGaming(context.Background(), hold)
	if !slices.Equal(fake.stopped, []string{"2"}) || holds != 2 {
		t.Fatalf("stopped = %v, holds = %d", fake.stopped, holds)
	}
	view, _ := service.Gaming(context.Background())
	if len(view.Sessions) != 1 || view.Sessions[0] != pc {
		t.Fatalf("sessions = %+v", view.Sessions)
	}

	// Ending the PC session from the page lets the TV play next.
	if err := service.StopGamingSession(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	fake.sessions = []agent.GamingSession{tv}
	service.checkGaming(context.Background(), hold)
	if len(fake.stopped) != 2 {
		t.Fatalf("stopped = %v", fake.stopped)
	}
	if view, _ := service.Gaming(context.Background()); len(view.Sessions) != 1 || view.Sessions[0] != tv {
		t.Fatalf("sessions = %+v", view.Sessions)
	}

	// A stopped container has no sessions.
	pve.resources[len(pve.resources)-1].Status = "stopped"
	service.checkGaming(context.Background(), hold)
	if service.gamingSessions != nil || service.gamingOwner != "" {
		t.Fatal("kept the sessions of a stopped container")
	}
}

// pairingAgent fakes the Moonlight devices that wait to pair with Wolf.
type pairingAgent struct {
	fakeAgent
	paired []string
}

func (f *pairingAgent) GamingPairings(_ context.Context, vmid int) ([]agent.GamingPairing, error) {
	return []agent.GamingPairing{{ID: "0123456789abcdef", ClientIP: "192.168.1.30"}}, nil
}

func (f *pairingAgent) PairGamingDevice(_ context.Context, vmid int, id, pin string) error {
	f.paired = append(f.paired, fmt.Sprintf("%d %s %s", vmid, id, pin))
	return nil
}

func TestGamingPairsADevice(t *testing.T) {
	gaming := proxmox.Resource{Type: "lxc", Node: "pve", VMID: 190, Name: "gaming", Status: "stopped", Tags: "iglo;gaming"}
	pve := &gamingProxmox{}
	pve.resources = append(baseResources(), gaming)
	fake := &pairingAgent{}
	changed := []string{}
	service := New(Options{Agent: fake, Proxmox: pve, SelfVMID: 100, Changed: func(topic string) { changed = append(changed, topic) }})
	ctx := context.Background()

	// Wolf runs only while you play.
	if pairings, err := service.GamingPairings(ctx); err != nil || len(pairings) != 0 {
		t.Fatalf("stopped: %v, %v", pairings, err)
	}
	if err := service.PairGamingDevice(ctx, "0123456789abcdef", "1234"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stopped pair: %v", err)
	}

	pve.resources[len(pve.resources)-1].Status = "running"
	if pairings, err := service.GamingPairings(ctx); err != nil || len(pairings) != 1 {
		t.Fatalf("running: %v, %v", pairings, err)
	}
	if err := service.PairGamingDevice(ctx, "0123456789abcdef", "12a4"); !errors.Is(err, agent.ErrInvalidAnswers) || len(fake.paired) != 0 {
		t.Fatalf("bad pin: %v, %v", err, fake.paired)
	}
	if err := service.PairGamingDevice(ctx, "0123456789abcdef", "1234"); err != nil || len(fake.paired) != 1 || fake.paired[0] != "190 0123456789abcdef 1234" {
		t.Fatalf("pair: %v, %v", err, fake.paired)
	}
	if !slices.Contains(changed, events.Gaming) {
		t.Fatalf("changed = %v", changed)
	}
}
