package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"iglo/internal/agent"
)

type onlineAgent struct{}

func (onlineAgent) Health(context.Context) (agent.Health, error) {
	return agent.Health{Status: "ok"}, nil
}

func TestReadyNeedsTheAgentAndLoopback(t *testing.T) {
	// The test client calls from 127.0.0.1, like the upgrade does.
	offline, _ := newTestServer(t)
	response, err := http.Get(offline.URL + "/api/ready")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("offline agent: %d", response.StatusCode)
	}

	online, _ := newTestServerWithOptions(t, func(options *Options) { options.Agent = onlineAgent{} })
	response, err = http.Get(online.URL + "/api/ready")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("online agent: %d", response.StatusCode)
	}

	// From the network it doesn't exist.
	handler := New(Options{Proxmox: &fakeProxmox{}, Agent: onlineAgent{}, Logger: slog.New(slog.DiscardHandler)})
	request := httptest.NewRequest(http.MethodGet, "/api/ready", nil)
	request.RemoteAddr = "192.168.1.30:52000"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("from the network: %d", recorder.Code)
	}
}
