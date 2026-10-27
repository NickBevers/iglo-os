package workloads

import (
	"context"
	"testing"
)

func TestHostMemoryComesFirstAndPressureHoldsWork(t *testing.T) {
	f := setup(t)
	memory := HostMemory{Node: "pve", Total: 32 << 30, Available: 16 << 30, Pressure: 0.5}
	f.s.hostMemory = func(context.Context) (HostMemory, error) { return memory, nil }
	reason := func() string {
		t.Helper()
		f.s.Reconcile(context.Background())
		return f.s.Status().Reason
	}

	if got := reason(); got != "Background processing allowed" || f.s.Status().FreeBytes != 16<<30 || f.s.Status().Pressure != 0.5 {
		t.Fatalf("reason = %q, view = %+v", got, f.s.Status())
	}
	// Tasks that wait for memory hold new work, also with memory to spare.
	memory.Pressure = 20
	if got := reason(); got != "Waiting for memory pressure to ease" {
		t.Fatalf("reason = %q", got)
	}
	// The agent's reading wins over Proxmox, which still says 8 GiB is free.
	memory.Pressure, memory.Available = 0, 3<<30
	if got := reason(); got != "Preserving RAM headroom" {
		t.Fatalf("reason = %q", got)
	}
	// An agent on another node doesn't count; Proxmox does.
	memory.Node = "other"
	if got := reason(); got != "Background processing allowed" || f.s.Status().Pressure != -1 {
		t.Fatalf("reason = %q, pressure = %v", got, f.s.Status().Pressure)
	}
}
