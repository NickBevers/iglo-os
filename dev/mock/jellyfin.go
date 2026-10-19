package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

const jellyfinAddr = "127.0.0.1:18096"

// newJellyfin fakes the parts of the Jellyfin API that the manager reads:
// the server info, the library counts, what plays now and what is new.
func newJellyfin() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /System/Info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ServerName": "jellyfin", "Version": "10.11.2"})
	})
	mux.HandleFunc("GET /System/Configuration/branding", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"LoginDisclaimer": "", "CustomCss": ""})
	})
	mux.HandleFunc("GET /Items/Counts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]int{
			"MovieCount": 412, "SeriesCount": 38, "EpisodeCount": 1290,
			"AlbumCount": 97, "SongCount": 1534, "MusicVideoCount": 12,
		})
	})
	mux.HandleFunc("GET /Items", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"Items": []map[string]any{
			{"Id": "frieren", "Name": "Frieren: Beyond Journey's End", "Type": "Series", "ProductionYear": 2023},
			{"Id": "spirited", "Name": "Spirited Away", "Type": "Movie", "ProductionYear": 2001},
			{"Id": "dune2", "Name": "Dune: Part Two", "Type": "Movie", "ProductionYear": 2024},
			{"Id": "samurai", "Name": "Blue Eye Samurai", "Type": "Series", "ProductionYear": 2023},
			{"Id": "perfect", "Name": "Perfect Days", "Type": "Movie", "ProductionYear": 2023},
		}})
	})
	mux.HandleFunc("GET /Sessions", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"Id": "1", "UserName": "nick", "Client": "Kodi", "DeviceName": "Living room",
				"NowPlayingItem": map[string]any{
					"Id": "frieren-e5", "Name": "Phantoms of the Dead", "Type": "Episode", "SeriesName": "Frieren: Beyond Journey's End",
					"SeriesId": "frieren", "ParentIndexNumber": 1, "IndexNumber": 5, "RunTimeTicks": 14_400_000_000,
				},
				"PlayState": map[string]any{"PositionTicks": 6_100_000_000, "IsPaused": false, "PlayMethod": "DirectPlay"},
			},
			{
				"Id": "2", "UserName": "nick", "Client": "Finamp", "DeviceName": "Phone",
				"NowPlayingItem": map[string]any{
					"Id": "song", "Name": "Merry-Go-Round of Life", "Type": "Audio", "RunTimeTicks": 3_000_000_000,
				},
				"PlayState": map[string]any{"PositionTicks": 1_200_000_000, "IsPaused": true, "PlayMethod": "DirectPlay"},
			},
		})
	})
	// Images are not faked, so the pages show their placeholders.
	return mux
}

// ensureJellyfin connects the manager to the fake Jellyfin once, unless
// .dev/no-jellyfin asks for a host without Jellyfin.
func ensureJellyfin() error {
	path := filepath.Join(dataDir, "jellyfin.json")
	if _, err := os.Stat(statePath("no-jellyfin")); err == nil {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	data, err := json.Marshal(map[string]string{"url": "http://" + jellyfinAddr, "apiKey": "dev"})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
