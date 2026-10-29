package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"iglo/internal/agent"
)

// sampleGames are real No-Intro and Redump names, so the box art comes from
// libretro like on a real server. The homebrew game has none, for the placeholder.
var sampleGames = map[string][]string{
	"gb":  {"Legend of Zelda, The - Link's Awakening (USA, Europe)", "Pokemon - Red Version (USA, Europe) (SGB Enhanced)", "Tetris (World) (Rev 1)"},
	"gbc": {"Pokemon - Crystal Version (USA, Europe) (Rev 1)", "Pokemon - Gold Version (USA, Europe) (SGB Enhanced) (GB Compatible)"},
	"gba": {"Mario Kart - Super Circuit (USA)", "Metroid Fusion (USA)", "Pokemon - Emerald Version (USA, Europe)", "Space Pong (World) (Homebrew)"},
	"n64": {"Legend of Zelda, The - Ocarina of Time (USA) (Rev 2)", "Mario Kart 64 (USA)", "Super Mario 64 (USA)"},
	"gc":  {"Legend of Zelda, The - The Wind Waker (USA)", "Metroid Prime (USA)", "Super Smash Bros. Melee (USA) (En,Ja) (Rev 2)"},
}

func sampleSystems() []agent.GameSystem {
	systems := []agent.GameSystem{}
	for _, system := range agent.GamingSystems {
		games := []agent.Game{}
		for _, name := range sampleGames[system.Folder] {
			games = append(games, agent.Game{Name: name, Title: agent.GameTitle(name)})
		}
		systems = append(systems, agent.GameSystem{ID: system.Folder, Label: system.Label, Games: games})
	}
	return systems
}

func sampleLibrary() []agent.Figure {
	figures := []agent.Figure{}
	for _, system := range sampleSystems() {
		figures = append(figures, agent.Figure{Label: system.Label, Value: int64(len(system.Games))})
	}
	return figures
}

// registerGaming fakes the game list. No game has a cover of its own here.
func registerGaming(mux *http.ServeMux) {
	// The fake host is prepared for Gaming, unless .dev/gaming-not-ready says otherwise.
	mux.HandleFunc("GET /v1/gaming/ready", func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(statePath("gaming-not-ready")); err == nil {
			writeJSON(w, agent.GamingReadiness{Missing: []string{"the uinput module (/dev/uinput)", "the Wolf input rules (/etc/udev/rules.d/85-wolf.rules)"}})
			return
		}
		writeJSON(w, agent.GamingReadiness{Ready: true, Missing: []string{}})
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/games", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, sampleSystems())
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/covers/{system}/{name}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, agent.ErrNoCover.Error(), http.StatusNotFound)
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/events", func(w http.ResponseWriter, r *http.Request) {
		streamChanges(w, r, "sessions", func() string { return fmt.Sprint(fakeSessions(), fakePairings()) })
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/pairing", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fakePairings())
	})
	mux.HandleFunc("POST /v1/gaming/{vmid}/pairing/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			PIN string `json:"pin"`
		}
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&input)
		if err := agent.CheckPairingAnswers(r.PathValue("id"), input.PIN); err != nil {
			http.Error(w, "give the PIN of four digits that Moonlight shows", http.StatusBadRequest)
			return
		}
		if len(fakePairings()) == 0 || r.PathValue("id") != fakePairingID {
			http.Error(w, agent.ErrNoGamingPairing.Error(), http.StatusConflict)
			return
		}
		os.WriteFile(statePath("gaming-paired"), nil, 0o600)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/sessions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fakeSessions())
	})
	mux.HandleFunc("POST /v1/gaming/{vmid}/sessions/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("id") {
		case "1001":
			os.WriteFile(statePath("gaming-pc-ended"), nil, 0o600)
		case "1002":
			os.Remove(statePath("gaming-second-session"))
		default:
			http.Error(w, "Wolf refused the request", http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// fakePairingID stands for the pair secret of the fake TV, as the agent's ID.
const fakePairingID = "5a1d0c3e9b7f2a64"

// fakePairings has a TV that waits to pair while the container runs, until
// it is paired (.dev/gaming-paired).
func fakePairings() []agent.GamingPairing {
	pairings := []agent.GamingPairing{}
	if _, err := os.Stat(statePath("gaming-running")); err != nil {
		return pairings
	}
	if _, err := os.Stat(statePath("gaming-paired")); err != nil {
		pairings = append(pairings, agent.GamingPairing{ID: fakePairingID, ClientIP: "192.168.1.70"})
	}
	return pairings
}

// fakeSessions plays a PC on Moonlight while the container runs. With
// .dev/gaming-second-session, a TV joins too, and the manager stops it.
func fakeSessions() []agent.GamingSession {
	sessions := []agent.GamingSession{}
	if _, err := os.Stat(statePath("gaming-running")); err != nil {
		return sessions
	}
	if _, err := os.Stat(statePath("gaming-pc-ended")); err != nil {
		sessions = append(sessions, agent.GamingSession{ID: "1001", ClientIP: "192.168.1.50", Width: 1920, Height: 1080, FPS: 60})
	}
	if _, err := os.Stat(statePath("gaming-second-session")); err == nil {
		sessions = append(sessions, agent.GamingSession{ID: "1002", ClientIP: "192.168.1.60", Width: 3840, Height: 2160, FPS: 60})
	}
	return sessions
}
