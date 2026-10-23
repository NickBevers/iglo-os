package apps

import (
	"context"
	"iglo/internal/agent"
	"iglo/internal/proxmox"
	"testing"
)

func TestScopedUpdateDoesNotSnapshotForDiskOnlyRollback(t *testing.T) {
	for _, id := range []string{agent.PhotosApp, agent.MusicApp, agent.InboxApp} {
		t.Run(id, func(t *testing.T) {
			fake := &fakeAgent{status: agent.AppInstallStatus{State: agent.UpgradeIdle}}
			pve := &fakeProxmox{resources: append(baseResources(), proxmox.Resource{Type: "lxc", Node: "pve", VMID: 170, Name: id, Tags: "iglo;" + id, Status: "running"})}
			s := New(Options{Agent: fake, Proxmox: pve, SelfVMID: 100})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := s.Update(ctx, ctx, id); err != nil {
				t.Fatal(err)
			}
			if len(pve.snapshots) != 0 {
				t.Fatal("made a snapshot for an unsafe disk-only rollback")
			}
		})
	}
}
