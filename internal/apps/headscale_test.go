package apps

import (
	"context"
	"testing"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
)

func TestControllerBudgetCountsStoppedGuestsAndPhysicalMemory(t *testing.T) {
	defaults := []int64{512, 3072, 4096, 8192, 1024, 2048, 6144}
	rows := []proxmox.Resource{{Type: "node", Node: "pve"}}
	for i, cap := range defaults {
		rows = append(rows, proxmox.Resource{Type: "lxc", Node: "pve", VMID: 100 + i, Status: "stopped", MaxMem: cap << 20})
	}
	memory := agent.HostMemory{Node: "pve", TotalBytes: 32 << 30, AvailableBytes: 16 << 30}
	if err := CheckHeadscaleBudget(rows, memory); err != nil {
		t.Fatal(err)
	}
	rows = append(rows, proxmox.Resource{Type: "qemu", Node: "pve", VMID: 300, Status: "stopped", MaxMem: 4 << 30})
	if err := CheckHeadscaleBudget(rows, memory); err == nil {
		t.Fatal("ignored stopped guest cap")
	}
	rows = rows[:len(rows)-1]
	memory.TotalBytes = 28 << 30
	if err := CheckHeadscaleBudget(rows, memory); err == nil {
		t.Fatal("assumed 32 GiB instead of measured memory")
	}
	memory.TotalBytes = 32 << 30
	memory.AvailableBytes = 4 << 30
	if err := CheckHeadscaleBudget(rows, memory); err == nil {
		t.Fatal("ignored actual headroom")
	}
	memory.AvailableBytes = 16 << 30
	rows[1].MaxMem = 0
	if err := CheckHeadscaleBudget(rows, memory); err == nil {
		t.Fatal("accepted unknown cap")
	}
}

func TestControllerVerifiedRecoveryDoesNotApplyOlderSnapshot(t *testing.T) {
	for _, code := range []int{1, 3, 4} {
		pve := &fakeProxmox{}
		s := New(Options{Agent: &fakeAgent{}, Proxmox: pve})
		s.finishUpdate(context.Background(), operation{app: App{ID: agent.HeadscaleApp, Name: "Headscale"}, guest: proxmox.Resource{VMID: 210, Node: "pve"}, snapshot: "before-download"}, agent.AppInstallStatus{State: agent.UpgradeFailed, ExitCode: code})
		want := 0
		if code == 1 {
			want = 1
		}
		if len(pve.rollbacks) != want {
			t.Fatalf("exit %d: got %d snapshot rollbacks, want %d", code, len(pve.rollbacks), want)
		}
	}
}
