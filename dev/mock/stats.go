package main

import (
	"net/http"
	"time"

	"iglo/internal/agent"
)

// plannedApps are the containers of the apps that Iglo-OS can't install yet.
// The fake host has them, so the dashboard shows them with their stats.
// With .dev/no-planned-apps, they are gone.
var plannedApps = []map[string]any{
	{"type": "lxc", "node": "pve", "vmid": 160, "name": "music", "status": "running", "tags": "iglo;music", "cpu": 0.04, "maxcpu": 2, "mem": 420_000_000, "maxmem": 1_073_741_824, "uptime": 86_400},
	{"type": "lxc", "node": "pve", "vmid": 170, "name": "photos", "status": "running", "tags": "iglo;photos", "cpu": 0.11, "maxcpu": 4, "mem": 3_100_000_000, "maxmem": 6_442_450_944, "uptime": 86_400},
	{"type": "lxc", "node": "pve", "vmid": 180, "name": "inbox", "status": "running", "tags": "iglo;inbox", "cpu": 0.62, "maxcpu": 4, "mem": 1_400_000_000, "maxmem": 2_147_483_648, "uptime": 86_400},
	// Gaming only runs during a session. Play on the Gaming page starts it.
	{"type": "lxc", "node": "pve", "vmid": 190, "name": "gaming", "status": "stopped", "tags": "iglo;gaming", "maxcpu": 4, "maxmem": 6_442_450_944},
}

// moving is a progress that goes from 0 to 100 in period, so the fake looks alive.
func moving(period time.Duration, offset time.Duration) int {
	return int(time.Now().Add(offset).UnixMilli() % period.Milliseconds() * 100 / period.Milliseconds())
}

func fakeStats(app string) (agent.AppStats, bool) {
	switch app {
	case "music":
		return agent.AppStats{
			Figures: []agent.Figure{
				{Label: "Songs", Value: 2341},
				{Label: "Albums", Value: 187},
				{Label: "Artists", Value: 96},
				{Label: "On disk", Value: 18_400_000_000, Unit: "bytes"},
			},
			Activity: []agent.Activity{
				{Title: "Kenshi Yonezu, Lemon", Detail: "Downloading from YouTube Music", Progress: moving(40*time.Second, 0)},
				{Title: "Fujii Kaze, Help Ever Hurt Never", Detail: "Waiting, 11 songs", Progress: -1},
			},
		}, true
	case "photos":
		return agent.AppStats{
			Figures: []agent.Figure{
				{Label: "Photos", Value: 18_422},
				{Label: "Videos", Value: 1204},
				{Label: "People", Value: 37},
				{Label: "On disk", Value: 214_000_000_000, Unit: "bytes"},
			},
			Activity: []agent.Activity{
				{Title: "Pixel 8", Detail: "Backed up 2 minutes ago", Progress: -1},
				{Title: "iPhone", Detail: "Backed up yesterday", Progress: -1},
				{Title: "Faces", Detail: "Recognizing new photos", Progress: 60 + moving(90*time.Second, 0)*4/10},
			},
		}, true
	case "inbox":
		return agent.AppStats{
			Figures: []agent.Figure{
				{Label: "Waiting", Value: 3},
				{Label: "Converted", Value: 27},
				{Label: "Failed", Value: 1},
				{Label: "Saved", Value: 41_000_000_000, Unit: "bytes"},
			},
			Activity: []agent.Activity{
				{Title: "family-2009.avi", Detail: "Converting to HEVC", Progress: moving(60*time.Second, 0)},
				{Title: "behind-the-scenes.mov", Detail: "Next", Progress: -1},
			},
		}, true
	case "gaming":
		// The library on the Games share, which the host counts even while the container is stopped.
		return agent.AppStats{
			Figures: []agent.Figure{
				{Label: "Game Boy", Value: 42},
				{Label: "Game Boy Color", Value: 31},
				{Label: "Game Boy Advance", Value: 58},
				{Label: "Nintendo 64", Value: 24},
				{Label: "GameCube", Value: 17},
				{Label: "Wii", Value: 12},
			},
			Activity: []agent.Activity{},
		}, true
	}

	return agent.AppStats{}, false
}

func serveStats(w http.ResponseWriter, r *http.Request) {
	stats, ok := fakeStats(r.PathValue("app"))
	if !ok {
		http.Error(w, agent.ErrNoStats.Error(), http.StatusNotFound)
		return
	}
	stats.Sample = true
	writeJSON(w, stats)
}
