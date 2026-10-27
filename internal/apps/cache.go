package apps

import (
	"sync"
	"time"
)

// ttlCache keeps answers that rarely change, like the version in the notes
// of a container, so the Apps view doesn't ask Proxmox for them every time.
// Errors are never kept. The zero value is ready to use.
type ttlCache[T any] struct {
	mu      sync.Mutex
	entries map[string]ttlEntry[T]
}

type ttlEntry[T any] struct {
	value T
	at    time.Time
}

func (c *ttlCache[T]) get(key string, ttl time.Duration, load func() (T, error)) (T, error) {
	c.mu.Lock()
	if entry, ok := c.entries[key]; ok && time.Since(entry.at) < ttl {
		c.mu.Unlock()
		return entry.value, nil
	}
	c.mu.Unlock()

	value, err := load()
	if err != nil {
		return value, err
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]ttlEntry[T]{}
	}
	c.entries[key] = ttlEntry[T]{value: value, at: time.Now()}
	c.mu.Unlock()
	return value, nil
}

func (c *ttlCache[T]) forget() {
	c.mu.Lock()
	c.entries = nil
	c.mu.Unlock()
}
