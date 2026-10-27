package main

import (
	"context"
	"encoding/json"
	"fmt"
	"iglo/internal/music"
	"net/http"
	"os"
	"path/filepath"
)

func addMusicRoutes(mux *http.ServeMux) error {
	library := statePath("music-library")
	if err := os.MkdirAll(library, 0700); err != nil {
		return err
	}
	library, _ = filepath.EvalSymlinks(library)
	worker, err := music.New(statePath("music-worker"), library)
	if err != nil {
		return err
	}
	lease := filepath.Join(worker.Dir, "admit-until")
	if _, err = os.Stat(lease); os.IsNotExist(err) {
		os.WriteFile(lease, []byte("-1"), 0600)
	}
	worker.MountOK = func() bool { return true }
	worker.Execute = func(_ context.Context, in music.Input, stage string) error {
		ext := ".m4a"
		if in.Video {
			ext = ".mp4"
		}
		return os.WriteFile(filepath.Join(stage, "clip"+ext), []byte("Development fixture; this is not a media file"), 0600)
	}
	go worker.Run(context.Background())
	handler := worker.Handler()
	mux.HandleFunc("GET /v1/music/{vmid}", func(w http.ResponseWriter, r *http.Request) { v := worker.Status(); v.Sample = true; writeJSON(w, v) })
	mux.HandleFunc("GET /v1/music/{vmid}/events", func(w http.ResponseWriter, r *http.Request) {
		streamChanges(w, r, "music", func() string { return fmt.Sprint(worker.Status()) })
	})
	for _, method := range []string{"POST", "PUT"} {
		path := "/v1/music/{vmid}"
		target := "/jobs"
		if method == "PUT" {
			path += "/pause"
			target = "/pause"
		}
		mux.HandleFunc(method+" "+path, func(w http.ResponseWriter, r *http.Request) {
			clone := r.Clone(r.Context())
			u := *r.URL
			u.Path = target
			clone.URL = &u
			handler.ServeHTTP(w, clone)
		})
	}
	mux.HandleFunc("PUT /v1/workers/{app}/{vmid}/admission", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Until int64 `json:"until"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "invalid lease", 400)
			return
		}
		if r.PathValue("app") == "music" {
			data, _ := json.Marshal(in.Until)
			if err := os.WriteFile(lease, data, 0600); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		w.WriteHeader(204)
	})
	return nil
}
