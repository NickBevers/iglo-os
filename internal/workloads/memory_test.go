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
	// The agent's reading counts, not Proxmox, which still says 8 GiB is free.
	memory.Pressure, memory.Available = 0, 3<<30
	if got := reason(); got != "Preserving RAM headroom" {
		t.Fatalf("reason = %q", got)
	}
	// An agent on another node doesn't count, and Proxmox is no stand-in:
	// new heavy work waits.
	memory.Node, memory.Available = "other", 16<<30
	if got := reason(); got != "Waiting for host memory" {
		t.Fatalf("other node: reason = %q", got)
	}
}

func TestNoAgentMeansNoReading(t *testing.T) {
	f := setup(t)
	f.s.hostMemory = nil
	if _, found := f.s.memory(context.Background(), "pve"); found {
		t.Fatal("found memory without the host agent")
	}
}

func TestBackupsHoldImmichQueues(t *testing.T) {
	f := setup(t)
	f.s.SetHold(func(context.Context) string { return "Updates are running" })
	f.s.Reconcile(context.Background())
	if f.s.Status().Reason != "Updates are running" || len(f.writes) == 0 {
		t.Fatalf("reason = %q, writes = %v", f.s.Status().Reason, f.writes)
	}
}
