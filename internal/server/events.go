package server

import (
	"fmt"
	"net/http"
	"time"
)

// keepAliveInterval is how often an idle event stream sends a comment, so
// proxies keep it open, and how often it checks that the session still holds.
const keepAliveInterval = 25 * time.Second

// serverEvents streams the topics that change to an open page, with
// Server-Sent Events. The page then fetches that topic the normal way. The
// browser reconnects by itself when the stream drops.
func (s *server) serverEvents(w http.ResponseWriter, r *http.Request) {
	if s.Events == nil {
		writeError(w, http.StatusServiceUnavailable, "update Iglo-OS for live updates")
		return
	}
	topics, stop := s.Events.Subscribe()
	defer stop()

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	controller := http.NewResponseController(w)
	fmt.Fprint(w, "retry: 5000\n\n")
	if controller.Flush() != nil {
		return
	}

	keepAlive := time.NewTicker(keepAliveInterval)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			// A stream that outlives its session, like after a logout, ends.
			if cookie, err := r.Cookie(sessionCookie); err != nil || !s.Auth.Validate(cookie.Value) {
				return
			}
			fmt.Fprint(w, ": keep-alive\n\n")
		case topic := <-topics:
			fmt.Fprintf(w, "data: %s\n\n", topic)
		}
		if controller.Flush() != nil {
			return
		}
	}
}
