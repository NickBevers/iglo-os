package apps

import (
	"context"
	"sync/atomic"
	"time"
)

// followRetry is how long a follower waits before it opens a stream again.
const followRetry = 5 * time.Second

// follower keeps an event stream of a container open while the container
// runs, through the host agent. Its events say "look again now", so the
// loop that owns it can wait long between its own checks while it is live.
type follower struct {
	open func(ctx context.Context, vmid int, onEvent func(topic string)) error
	// events wakes the owner. It holds one wake-up, so a burst becomes one.
	events chan struct{}
	vmid   int
	cancel context.CancelFunc
	live   atomic.Bool
}

func newFollower(open func(ctx context.Context, vmid int, onEvent func(topic string)) error) *follower {
	return &follower{open: open, events: make(chan struct{}, 1)}
}

// keep follows vmid while it runs, and stops following otherwise.
func (f *follower) keep(ctx context.Context, vmid int, running bool) {
	if running && f.cancel != nil && f.vmid == vmid {
		return
	}
	f.stop()
	if !running {
		return
	}
	streamCtx, cancel := context.WithCancel(ctx)
	f.cancel, f.vmid = cancel, vmid
	go func() {
		for streamCtx.Err() == nil {
			// An empty topic means the stream opened. Things may have changed
			// while it was closed, so that wakes the owner too.
			f.open(streamCtx, vmid, func(topic string) {
				f.live.Store(true)
				select {
				case f.events <- struct{}{}:
				default:
				}
			})
			f.live.Store(false)
			select {
			case <-streamCtx.Done():
			case <-time.After(followRetry):
			}
		}
	}()
}

func (f *follower) stop() {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.live.Store(false)
}
