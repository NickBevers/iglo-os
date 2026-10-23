package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"iglo/internal/agent"
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

func (f *fakeGamingApps) GamingGames(context.Context) ([]agent.GameSystem, error) {
	return []agent.GameSystem{{ID: "gba", Label: "Game Boy Advance", Games: []agent.Game{{Name: "Metroid Fusion (USA)", Title: "Metroid Fusion", Cover: true}}}}, nil
}

func (f *fakeGamingApps) GamingOwnCover(_ context.Context, system, name string) ([]byte, string, error) {
	if system == "gba" && name == "Metroid Fusion (USA)" {
		return []byte("own"), "image/jpeg", nil
	}
	return nil, "", agent.ErrNoCover
}

type fakeCovers struct{ asked []string }

func (f *fakeCovers) Cover(_ context.Context, system, name string) ([]byte, error) {
	f.asked = append(f.asked, system+"/"+name)
	if name == "Wii Sports (USA)" {
		return []byte("libretro"), nil
	}
	return nil, errors.New("no cover found")
}

func TestGameCoversPreferTheShare(t *testing.T) {
	libretro := &fakeCovers{}
	server, _ := newTestServerWithOptions(t, func(options *Options) {
		options.Apps = &fakeGamingApps{}
		options.Covers = libretro
	})
	cookie := login(t, server)

	cover := func(path string) (int, string, string) {
		t.Helper()
		response := request(t, http.MethodGet, server.URL+path, "", cookie)
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return response.StatusCode, response.Header.Get("Content-Type"), string(data)
	}
	if code, kind, body := cover("/api/gaming/covers/gba/Metroid%20Fusion%20(USA)?own=1"); code != 200 || kind != "image/jpeg" || body != "own" {
		t.Fatalf("own cover: %d %s %q", code, kind, body)
	}
	if len(libretro.asked) != 0 {
		t.Fatalf("asked libretro for an own cover: %v", libretro.asked)
	}
	if code, kind, body := cover("/api/gaming/covers/wii/Wii%20Sports%20(USA)"); code != 200 || kind != "image/jpeg" || body != "libretro" {
		t.Fatalf("libretro cover: %d %s %q", code, kind, body)
	}
	if code, _, _ := cover("/api/gaming/covers/n64/Homebrew"); code != http.StatusNotFound {
		t.Fatalf("no cover: %d", code)
	}
	for _, path := range []string{"/api/gaming/covers/snes/Tetris", "/api/gaming/covers/gba/..", "/api/gaming/covers/gba/.hidden"} {
		if code, _, _ := cover(path); code != http.StatusBadRequest && code != http.StatusNotFound && code != http.StatusMovedPermanently {
			t.Errorf("%s: %d", path, code)
		}
	}
	if code, _, _ := cover("/api/gaming/covers/snes/Tetris"); code != http.StatusBadRequest {
		t.Errorf("unknown system: %d", code)
	}

	response := request(t, http.MethodGet, server.URL+"/api/gaming/games", "", cookie)
	defer response.Body.Close()
	var games struct{ Systems []agent.GameSystem }
	if err := json.NewDecoder(response.Body).Decode(&games); err != nil || len(games.Systems) != 1 || games.Systems[0].Games[0].Title != "Metroid Fusion" {
		t.Fatalf("games = %+v, %v", games, err)
	}
}
