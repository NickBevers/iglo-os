package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"iglo/internal/apps"
	"iglo/internal/workloads"
)

// fakeGamingApps only answers the gaming calls.
type fakeGamingApps struct {
	Apps
	running bool
	fail    error
}

func (f *fakeGamingApps) Gaming(context.Context) (apps.GamingView, error) {
	status := "stopped"
	if f.running {
		status = "running"
	}
	return apps.GamingView{Installed: true, VMID: 190, Status: status}, nil
}

func (f *fakeGamingApps) SetGamingPower(_ context.Context, on bool) error {
	if f.fail != nil {
		return f.fail
	}
	f.running = on
	return nil
}

func TestPlayStartsGamingWithProtection(t *testing.T) {
	control, err := workloads.New(workloads.Options{DataDir: t.TempDir(), Proxmox: &fakeProxmox{}})
	if err != nil {
		t.Fatal(err)
	}
	gaming := &fakeGamingApps{}
	server, _ := newTestServerWithOptions(t, func(options *Options) {
		options.Apps = gaming
		options.Workloads = control
	})
	cookie := login(t, server)

	status := func() apps.GamingView {
		t.Helper()
		response := request(t, http.MethodGet, server.URL+"/api/gaming", "", cookie)
		defer response.Body.Close()
		var view apps.GamingView
		if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
			t.Fatal(err)
		}
		return view
	}

	if response := request(t, http.MethodPut, server.URL+"/api/gaming/play", `{"play":true}`, cookie); response.StatusCode != http.StatusNoContent {
		t.Fatalf("play: %d", response.StatusCode)
	}
	if view := status(); view.Status != "running" || !view.Protected || view.ProtectedUntil.IsZero() {
		t.Fatalf("after play: %+v", view)
	}

	if response := request(t, http.MethodPut, server.URL+"/api/gaming/play", `{"play":false}`, cookie); response.StatusCode != http.StatusNoContent {
		t.Fatalf("stop: %d", response.StatusCode)
	}
	if view := status(); view.Status != "stopped" || view.Protected {
		t.Fatalf("after stop: %+v", view)
	}

	// A start that fails doesn't leave protection behind.
	gaming.fail = errors.New("Proxmox refused")
	if response := request(t, http.MethodPut, server.URL+"/api/gaming/play", `{"play":true}`, cookie); response.StatusCode != http.StatusBadGateway {
		t.Fatalf("failed play: %d", response.StatusCode)
	}
	if control.Status().GamingActive {
		t.Fatal("protection stayed on after a failed start")
	}

	if response := request(t, http.MethodPut, server.URL+"/api/gaming/play", `{}`, cookie); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty body: %d", response.StatusCode)
	}
}
