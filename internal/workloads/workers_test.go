package workloads

import (
	"context"
	"testing"
	"time"
)

func TestWorkerLeasesIndependentOfImmich(t *testing.T) {
	now := time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)
	host := &fakeHost{free: 8 << 30}
	lease := int64(-99)
	s, err := New(Options{DataDir: t.TempDir(), Proxmox: host, Now: func() time.Time { return now }, WorkerAdmission: func(_ context.Context, _ string, until int64) error { lease = until; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings()
	settings.Workers = true
	settings.Node = "pve"
	settings.Window = true
	if err = s.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	s.Reconcile(context.Background())
	if lease != now.Add(time.Minute).Unix() {
		t.Fatalf("lease=%d", lease)
	}
	if !s.busy {
		t.Fatal("a lease must be renewed at the busy pace")
	}
	s.Protect(120)
	s.Reconcile(context.Background())
	if lease != 0 {
		t.Fatal("admitted while gaming")
	}
	if s.busy {
		t.Fatal("stayed busy with nothing admitted")
	}
	s.Protect(0)
	host.free = 2 << 30
	s.Reconcile(context.Background())
	if lease != 0 {
		t.Fatal("admitted without headroom")
	}
	host.free = 8 << 30
	now = now.Add(10 * time.Hour)
	s.Reconcile(context.Background())
	if lease != 0 {
		t.Fatal("admitted outside window")
	}
	settings.Workers = false
	s.SaveSettings(settings)
	s.Reconcile(context.Background())
	if lease != -1 {
		t.Fatal("did not return to native controls")
	}
}
