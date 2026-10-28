// Package events tells the open pages what changed, so they ask for it at
// once instead of waiting for their next poll. An event only names a topic,
// like "apps"; the page then fetches the data the normal way.
package events

import "sync"

// Topics that the pages know.
const (
	Health    = "health"
	Updates   = "updates"
	Backups   = "backups"
	Apps      = "apps"
	Gaming    = "gaming"
	Workloads = "workloads"
	Music     = "music"
	Inbox     = "inbox"
	Jellyfin  = "jellyfin"
	Setup     = "setup"
)

// Hub passes topics from the services to the open pages.
type Hub struct {
	mu   sync.Mutex
	subs map[chan string]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[chan string]struct{}{}}
}

// Publish tells every page that topic changed. It never blocks: a page that
// can't keep up misses a topic, and its poll catches up.
func (h *Hub) Publish(topic string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		select {
		case sub <- topic:
		default:
		}
	}
}

// Subscribe returns the topics for one page, and a function that ends them.
func (h *Hub) Subscribe() (<-chan string, func()) {
	sub := make(chan string, 32)
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub, func() {
		h.mu.Lock()
		delete(h.subs, sub)
		h.mu.Unlock()
	}
}
