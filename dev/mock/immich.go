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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
