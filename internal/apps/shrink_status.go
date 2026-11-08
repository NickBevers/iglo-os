package apps

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"iglo/internal/shrink"
)

const (
	shrinkStatusTTL = 30 * time.Second
	shrinkPoll      = time.Minute
	shrinkMaxRetry  = 30 * time.Minute
)

// shrinkRetry logs only transitions. Repeated failures back off even when
// their message stays the same. All callers serialize access to it.
type shrinkRetry struct {
	message  string
	interval time.Duration
	next     time.Time
}

func (r *shrinkRetry) failed(logger *slog.Logger, err error) time.Duration {
	if r.message != err.Error() {
		logger.Warn("library shrink unavailable", "error", err)
	}
	r.message = err.Error()
	if r.interval == 0 {
		r.interval = shrinkPoll
	} else {
		r.interval = min(r.interval*2, shrinkMaxRetry)
	}
	r.next = time.Now().Add(r.interval)
	return r.interval
}
func (r *shrinkRetry) recovered(logger *slog.Logger) {
	if r.message != "" {
		logger.Info("library shrink available")
	}
	*r = shrinkRetry{}
}
func (r *shrinkRetry) remaining() time.Duration { return max(0, time.Until(r.next)) }

// The watcher and all tabs share one worker read per 30 seconds. An in-flight
// read is shared too. User actions and stream reconnection forget a snapshot;
// progress events wait for its expiry instead of starting another pct exec.
// Failures are shared and retried after 1, 2, 4, 8, 16 and then 30 minutes.
type shrinkStatusCache struct {
	mu         sync.Mutex
	vmid       int
	view       shrink.View
	at         time.Time
	generation uint64
	loading    chan struct{}
	err        error
	retry      shrinkRetry
}

func (c *shrinkStatusCache) forget(force bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = time.Time{}
	c.generation++
	if force {
		c.retry.next = time.Time{}
		c.retry.interval = 0
	}
}
func (c *shrinkStatusCache) remaining() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.retry.remaining()
	}
	if c.at.IsZero() {
		return 0
	}
	return max(0, time.Until(c.at.Add(shrinkStatusTTL)))
}
func (c *shrinkStatusCache) acknowledged(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.view.Jobs {
		if c.view.Jobs[i].ID == id {
			c.view.Jobs[i].Rescanned = true
		}
	}
}
func copyShrinkView(view shrink.View) shrink.View { view.Jobs = slices.Clone(view.Jobs); return view }
func (c *shrinkStatusCache) get(ctx context.Context, a shrinkAgent, id int, logger *slog.Logger) (shrink.View, error, bool) {
	for {
		c.mu.Lock()
		if c.loading != nil {
			wait := c.loading
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return shrink.View{}, ctx.Err(), false
			case <-wait:
				continue
			}
		}
		if c.vmid != id {
			c.vmid = id
			c.at = time.Time{}
			c.err = nil
			c.retry = shrinkRetry{}
			c.generation++
		}
		if c.err != nil && c.retry.remaining() > 0 {
			err := c.err
			c.mu.Unlock()
			return shrink.View{}, err, true
		}
		if !c.at.IsZero() && time.Since(c.at) < shrinkStatusTTL {
			view := copyShrinkView(c.view)
			c.mu.Unlock()
			return view, nil, true
		}
		c.loading = make(chan struct{})
		generation := c.generation
		c.mu.Unlock()
		view, err := a.ShrinkJobs(ctx, id)
		c.mu.Lock()
		if ctx.Err() != context.Canceled {
			if err != nil {
				c.err = err
				c.retry.failed(logger, err)
			} else {
				c.err = nil
				c.retry.recovered(logger)
				if generation == c.generation {
					c.view = copyShrinkView(view)
					c.at = time.Now()
				}
			}
		}
		close(c.loading)
		c.loading = nil
		c.mu.Unlock()
		return view, err, false
	}
}
