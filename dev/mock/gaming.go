package main

import (
	"net/http"

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
}
