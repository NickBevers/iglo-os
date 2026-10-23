package apps

import (
	"context"
	"testing"

	"iglo/internal/agent"
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
