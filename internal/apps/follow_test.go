package apps

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestFollowerFollowsWhileTheContainerRuns(t *testing.T) {
	var opened atomic.Int32
	follow := newFollower(func(ctx context.Context, vmid int, onEvent func(string)) error {
		if vmid != 190 {
			t.Errorf("vmid = %d", vmid)
		}
		opened.Add(1)
		onEvent("")
		onEvent("sessions")
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	follow.keep(ctx, 190, true)
	select {
	case <-follow.events:
	case <-time.After(2 * time.Second):
		t.Fatal("no wake-up")
	}
	if !follow.live.Load() {
		t.Fatal("not live while open")
	}
	// The same container keeps its stream.
	follow.keep(ctx, 190, true)
	time.Sleep(50 * time.Millisecond)
	if opened.Load() != 1 {
		t.Fatalf("opened %d times", opened.Load())
	}

	follow.keep(ctx, 0, false)
	if follow.live.Load() || follow.cancel != nil {
		t.Fatal("still following a stopped container")
	}
}
