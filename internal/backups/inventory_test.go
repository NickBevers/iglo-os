package backups

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iglo/internal/proxmox"
)

type countingInventory struct {
	*fakeProxmox
	storageCalls atomic.Int32
	archiveCalls atomic.Int32
	fail         atomic.Bool
	delay        time.Duration
}

func (p *countingInventory) BackupStorages(ctx context.Context, node string) ([]proxmox.BackupStorage, error) {
	p.storageCalls.Add(1)
	return p.fakeProxmox.BackupStorages(ctx, node)
}

func (p *countingInventory) Backups(ctx context.Context, node, storage string) ([]proxmox.Backup, error) {
	p.archiveCalls.Add(1)
	time.Sleep(p.delay)
	if p.fail.Load() {
		return nil, errors.New("offline")
	}
	return p.fakeProxmox.Backups(ctx, node, storage)
}

func TestInventorySharesConcurrentReadsAndInvalidates(t *testing.T) {
	h := newHarness(t)
	pve := &countingInventory{fakeProxmox: h.pve, delay: 20 * time.Millisecond}
	h.service.Proxmox = pve
	var group sync.WaitGroup
	for range 10 {
		group.Go(func() {
			view, err := h.service.Status(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			// The returned view must not mutate the shared inventory.
			view.Guests[1].Backups[0].Notes = "changed"
		})
	}
	group.Wait()
	if pve.storageCalls.Load() != 1 || pve.archiveCalls.Load() != 2 {
		t.Fatal(pve.storageCalls.Load(), pve.archiveCalls.Load())
	}
	view, _ := h.service.Status(context.Background())
	if view.Guests[1].Backups[0].Notes == "changed" {
		t.Fatal("shared mutable inventory")
	}
	h.service.forgetInventory()
	h.service.Status(context.Background())
	if pve.archiveCalls.Load() != 4 {
		t.Fatal("invalidation did not refresh")
	}
}

func TestPartialInventoryIsNeverCached(t *testing.T) {
	h := newHarness(t)
	pve := &countingInventory{fakeProxmox: h.pve}
	h.service.Proxmox = pve
	pve.fail.Store(true)
	h.service.Status(context.Background())
	pve.fail.Store(false)
	view, err := h.service.Status(context.Background())
	if err != nil || len(view.Guests[1].Backups) == 0 || pve.archiveCalls.Load() != 4 {
		t.Fatalf("partial result cached: %+v, %v", view, err)
	}
}

func TestInventoryInvalidationDuringLoadCannotRepopulateOldGeneration(t *testing.T) {
	h := newHarness(t)
	pve := &countingInventory{fakeProxmox: h.pve, delay: 30 * time.Millisecond}
	h.service.Proxmox = pve
	done := make(chan struct{})
	go func() { defer close(done); h.service.Status(context.Background()) }()
	for pve.archiveCalls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	h.service.forgetInventory()
	<-done
	h.service.Status(context.Background())
	if pve.archiveCalls.Load() != 4 {
		t.Fatal("stale in-flight result repopulated cache")
	}
}
