package main

import (
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
	"wii": {"Mario Kart Wii (USA) (En,Fr,Es)", "Super Mario Galaxy (USA) (En,Fr,Es)", "Wii Sports (USA)"},
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
	mux.HandleFunc("GET /v1/gaming/{vmid}/games", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, sampleSystems())
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/covers/{system}/{name}", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, agent.ErrNoCover.Error(), http.StatusNotFound)
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
