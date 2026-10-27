package workloads

import (
	"context"
	"testing"
	"time"

	"iglo/internal/proxmox"
)

// guestHost has a Photos container at 192.168.1.30 on node pve2.
type guestHost struct {
	fakeHost
	lookups int
}

func (h *guestHost) Resources(context.Context) ([]proxmox.Resource, error) {
	return []proxmox.Resource{
		{Type: "node", Node: "pve", Status: "online", MaxMem: 32 << 30, Mem: 8 << 30},
		{Type: "node", Node: "pve2", Status: "online", MaxMem: 32 << 30, Mem: 8 << 30},
		{Type: "lxc", Node: "pve", VMID: 101, Name: "jellyfin", Status: "running"},
		{Type: "lxc", Node: "pve2", VMID: 170, Name: "photos", Status: "running"},
		{Type: "lxc", Node: "pve2", VMID: 171, Name: "old-photos", Status: "stopped"},
	}, nil
}

func (h *guestHost) ContainerInterfaces(_ context.Context, node string, vmid int) ([]proxmox.Interface, error) {
	h.lookups++
	if vmid == 171 {
		panic("looked inside a stopped container")
	}
	address := map[int]string{101: "192.168.1.20/24", 170: "192.168.1.30/24"}[vmid]
	return []proxmox.Interface{{Name: "lo", Inet: "127.0.0.1/8"}, {Name: "eth0", Inet: address}}, nil
}

func TestImmichNodeIsFoundAndChecked(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	host := &guestHost{}
	s, err := New(Options{DataDir: t.TempDir(), Proxmox: host, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	// After a Photos install, the URL is known and the node is found.
	if err := s.Discover("http://192.168.1.30:2283"); err != nil {
		t.Fatal(err)
	}
	s.Reconcile(context.Background())
	view := s.Status()
	if view.Settings.Node != "pve2" || view.ImmichGuest != "photos (170) on pve2" || view.NodeWarning != "" {
		t.Fatalf("view = %+v", view)
	}

	// A chosen node is never changed, but the page warns.
	settings := view.Settings
	settings.Node = "pve"
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	s.Reconcile(context.Background())
	if view := s.Status(); view.Settings.Node != "pve" || view.NodeWarning == "" {
		t.Fatalf("view = %+v", view)
	}

	// The search runs again only after a while, or for a new URL.
	lookups := host.lookups
	s.Reconcile(context.Background())
	if host.lookups != lookups {
		t.Fatal("looked again right away")
	}
	now = now.Add(31 * time.Minute)
	s.Reconcile(context.Background())
	if host.lookups == lookups {
		t.Fatal("never looked again")
	}

	// Immich outside Proxmox, like on a NAS: no guest and no warning.
	settings = s.Status().Settings
	settings.URL = "http://192.168.1.40:2283"
	if err := s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	s.Reconcile(context.Background())
	if view := s.Status(); view.ImmichGuest != "" || view.NodeWarning != "" || view.Settings.Node != "pve" {
		t.Fatalf("view = %+v", view)
	}
}
