package main

import (
	"encoding/json"
	"log"
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
	// The first admin, so a login with other credentials fails like in Immich.
	var admin struct{ Email, Password string }
	config := map[string]any{
		"job":             map[string]any{"videoConversion": map[string]any{"concurrency": 1}, "faceDetection": map[string]any{"concurrency": 2}, "smartSearch": map[string]any{"concurrency": 2}, "ocr": map[string]any{"concurrency": 1}},
		"ffmpeg":          map[string]any{"threads": 0, "transcode": "optimal"},
		"machineLearning": map[string]any{"facialRecognition": map[string]any{"enabled": true, "modelName": "buffalo_l"}},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The first admin and its API key, for the setup and for a Photos
		// install from the Apps page. The key is the dev key.
		switch r.Method + " " + r.URL.Path {
		case "GET /api/server/config":
			mu.Lock()
			json.NewEncoder(w).Encode(map[string]bool{"isInitialized": initialized})
			mu.Unlock()
			return
		case "POST /api/auth/admin-sign-up":
			var body struct{ Email, Password, Name string }
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil || body.Email == "" || body.Password == "" || body.Name == "" {
				http.Error(w, "email, password and name are required", 400)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if initialized {
				http.Error(w, "The server already has an admin", 400)
				return
			}
			initialized = true
			admin.Email, admin.Password = body.Email, body.Password
			log.Println("fake Immich: made the admin", body.Email)
			w.WriteHeader(http.StatusCreated)
			return
		case "POST /api/auth/login":
			var body struct{ Email, Password string }
			json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
			mu.Lock()
			defer mu.Unlock()
			if !initialized || body.Email != admin.Email || body.Password != admin.Password {
				http.Error(w, "Incorrect email or password", 401)
				return
			}
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
			var body struct {
				Permissions []string `json:"permissions"`
			}
			if r.Header.Get("Authorization") != "Bearer dev-token" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
				http.Error(w, "admin token required", 401)
				return
			}
			log.Println("fake Immich: made an API key with", strings.Join(body.Permissions, " and "))
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
