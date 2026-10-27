package agent

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWolfSessionEventsArePicked(t *testing.T) {
	for name, want := range map[string]bool{
		"wolf::core::events::StreamSession":     true,
		"wolf::core::events::StopStreamEvent":   true,
		"wolf::core::events::PauseStreamEvent":  true,
		"wolf::core::events::ResumeStreamEvent": true,
		"StopStreamEvent":                       true,
		"wolf::core::events::VideoSession":      false,
		"wolf::core::events::PlugDeviceEvent":   false,
		"":                                      false,
	} {
		if wolfSessionEvent(name) != want {
			t.Errorf("%q: want %v", name, want)
		}
	}
}

func TestEventsSayWhenAnInstallMovesOn(t *testing.T) {
	installer := &AppInstaller{Dir: t.TempDir()}
	mux := http.NewServeMux()
	RegisterEvents(mux, installer)
	server := httptest.NewServer(mux)
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	lines := bufio.NewReader(response.Body)
	if line, _ := lines.ReadString('\n'); line != ": keep-alive\n" {
		t.Fatalf("first line = %q", line)
	}
	lines.ReadString('\n')

	os.WriteFile(installer.logPath(), []byte("==> Creating the container\n"), 0o600)
	done := make(chan string, 1)
	go func() {
		line, _ := lines.ReadString('\n')
		done <- line
	}()
	select {
	case line := <-done:
		if strings.TrimSpace(line) != "data: apps" {
			t.Fatalf("line = %q", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event for the new log")
	}
}

func TestFollowReadsTopicsAndSaysWhenOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/gaming/190/events" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(": keep-alive\n\ndata: sessions\n\ndata: \n\n"))
	}))
	defer server.Close()
	// The client always asks http://agent, so send it to the test server.
	client := &Client{stream: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
		},
	}}}

	topics := []string{}
	err := client.GamingEvents(context.Background(), 190, func(topic string) { topics = append(topics, topic) })
	if err == nil || !slices.Equal(topics, []string{"", "sessions"}) {
		t.Fatalf("topics = %q, err = %v", topics, err)
	}
	if err := client.GamingEvents(context.Background(), 191, func(string) {}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}
