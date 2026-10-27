package main

import (
	"fmt"
	"net/http"
	"time"
)

// streamChanges fakes an event stream of the agent: it sends topic each time
// fingerprint changes. The real agent relays the events of Wolf or the worker.
func streamChanges(w http.ResponseWriter, r *http.Request, topic string, fingerprint func() string) {
	w.Header().Set("Content-Type", "text/event-stream")
	controller := http.NewResponseController(w)
	last := fingerprint()
	fmt.Fprint(w, ": open\n\n")
	check := time.NewTicker(time.Second)
	defer check.Stop()
	for controller.Flush() == nil {
		select {
		case <-r.Context().Done():
			return
		case <-check.C:
			if current := fingerprint(); current != last {
				last = current
				fmt.Fprintf(w, "data: %s\n\n", topic)
			} else {
				fmt.Fprint(w, ": keep-alive\n\n")
			}
		}
	}
}
