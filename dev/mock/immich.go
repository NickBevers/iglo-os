package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"iglo/internal/workloads"
)

const immichAddr = "127.0.0.1:18283"

// This is a queue protocol fixture, not an Immich installation or performance test.
func newImmich() http.Handler {
	var mu sync.Mutex
	queues := []workloads.Queue{}
	for _, name := range append([]string{"metadataExtraction", "thumbnailGeneration", "backupDatabase"}, workloads.HeavyQueues...) {
		q := workloads.Queue{Name: name}
		q.Statistics.Waiting = 12
		queues = append(queues, q)
	}
	initialized := false
	config := map[string]any{
		"job":             map[string]any{"videoConversion": map[string]any{"concurrency": 1}, "faceDetection": map[string]any{"concurrency": 2}, "smartSearch": map[string]any{"concurrency": 2}, "ocr": map[string]any{"concurrency": 1}},
		"ffmpeg":          map[string]any{"threads": 0, "transcode": "optimal"},
		"machineLearning": map[string]any{"facialRecognition": map[string]any{"enabled": true, "modelName": "buffalo_l"}},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The first admin and its API key, for the setup. The key is the dev key.
		switch r.Method + " " + r.URL.Path {
		case "GET /api/server/config":
			mu.Lock()
			json.NewEncoder(w).Encode(map[string]bool{"isInitialized": initialized})
			mu.Unlock()
			return
		case "POST /api/auth/admin-sign-up":
			mu.Lock()
			initialized = true
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			return
		case "POST /api/auth/login":
			json.NewEncoder(w).Encode(map[string]string{"accessToken": "dev-token"})
			return
		case "GET /api/admin/config", "PUT /api/admin/config":
			if r.Header.Get("Authorization") != "Bearer dev-token" {
				http.Error(w, "admin token required", 401)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if r.Method == http.MethodPut {
				var next map[string]any
				if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&next) != nil {
					http.Error(w, "bad config", 400)
					return
				}
				config = next
			}
			json.NewEncoder(w).Encode(config)
			return
		case "POST /api/api-keys":
			json.NewEncoder(w).Encode(map[string]string{"secret": "iglo-dev-immich"})
			return
		}
		if r.Header.Get("x-api-key") != "iglo-dev-immich" {
			http.Error(w, "invalid dev key", http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/queues" && r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(queues)
			return
		}
		if r.Method == http.MethodPut {
			name := strings.TrimPrefix(r.URL.Path, "/api/queues/")
			for index := range queues {
				if queues[index].Name == name {
					var body struct {
						IsPaused bool `json:"isPaused"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						http.Error(w, "bad request", 400)
						return
					}
					queues[index].IsPaused = body.IsPaused
					json.NewEncoder(w).Encode(queues[index])
					return
				}
			}
		}
		http.NotFound(w, r)
	})
}
