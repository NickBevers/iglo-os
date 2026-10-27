package server

import (
	"bufio"
	"net/http"
	"strings"
	"testing"

	"iglo/internal/events"
)

func TestEventsStreamTopicsToALoggedInPage(t *testing.T) {
	hub := events.NewHub()
	server, _ := newTestServerWithOptions(t, func(options *Options) { options.Events = hub })

	if response := request(t, http.MethodGet, server.URL+"/api/events", "", nil); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", response.StatusCode)
	}

	response := request(t, http.MethodGet, server.URL+"/api/events", "", login(t, server))
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status = %d, headers = %v", response.StatusCode, response.Header)
	}
	lines := bufio.NewReader(response.Body)
	if line, _ := lines.ReadString('\n'); !strings.HasPrefix(line, "retry:") {
		t.Fatalf("first line = %q", line)
	}
	lines.ReadString('\n')

	hub.Publish(events.Apps)
	if line, _ := lines.ReadString('\n'); line != "data: apps\n" {
		t.Fatalf("line = %q", line)
	}
}
