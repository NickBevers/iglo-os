package main

import (
	"context"
	"fmt"
	"iglo/internal/inbox"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// inboxLease is the Inbox worker's admit-until file.
var inboxLease string

func addInboxRoutes(mux *http.ServeMux) error {
	source, output := statePath("inbox-source"), statePath("inbox-output")
	for _, p := range []string{source, output} {
		if err := os.MkdirAll(p, 0700); err != nil {
			return err
		}
	}
	s, err := inbox.New(statePath("inbox-worker"), source, output)
	if err != nil {
		return err
	}
	s.MountOK = func() bool { return true }
	s.FreeBytes = func() (uint64, error) { return 1 << 40, nil }
	os.MkdirAll(s.AdmissionDir, 0700)
	// The shared admission route in music.go writes this lease too, so the
	// mock scans by the same rules as the real worker. Like the installer,
	// start at -1 only when there is no lease yet.
	inboxLease = filepath.Join(s.AdmissionDir, "admit-until")
	if _, err = os.Stat(inboxLease); os.IsNotExist(err) {
		os.WriteFile(inboxLease, []byte("-1"), 0600)
	}
	s.Execute = func(ctx context.Context, source, stage, _ string) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return "", err
		}
		path := filepath.Join(stage, "output.mkv")
		return path, os.WriteFile(path, data, 0600)
	}
	go s.Run(context.Background())
	handler := s.Handler()
	mux.HandleFunc("GET /v1/inbox/{vmid}", func(w http.ResponseWriter, r *http.Request) {
		v := s.Status()
		v.Sample = true
		writeJSON(w, v)
	})
	mux.HandleFunc("GET /v1/inbox/{vmid}/events", func(w http.ResponseWriter, r *http.Request) {
		streamChanges(w, r, "inbox", func() string { return fmt.Sprint(s.Status()) })
	})
	for _, route := range []struct{ pattern, target string }{{"POST /v1/inbox/{vmid}", "/jobs"}, {"PUT /v1/inbox/{vmid}/settings", "/settings"}, {"POST /v1/inbox/{vmid}/scan", "/scan"}, {"POST /v1/inbox/{vmid}/retry/{id}", ""}} {
		mux.HandleFunc(route.pattern, func(w http.ResponseWriter, r *http.Request) {
			clone := r.Clone(r.Context())
			u := *r.URL
			u.Path = route.target
			if u.Path == "" {
				u.Path = "/jobs/" + r.PathValue("id") + "/retry"
			}
			clone.URL = &u
			handler.ServeHTTP(w, clone)
		})
	}
	return nil
}
