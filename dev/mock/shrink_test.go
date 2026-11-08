package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"iglo/internal/inbox"
	"iglo/internal/shrink"
	"iglo/internal/stacks/arr"
)

func TestLibraryShrinkMockFlow(t *testing.T) {
	t.Chdir(t.TempDir())
	admission := t.TempDir()
	os.WriteFile(filepath.Join(admission, "admit-until"), []byte("-1"), 0600)
	mux := http.NewServeMux()
	s, err := addShrinkRoutes(mux, &inbox.Service{AdmissionDir: admission})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	call := func(method, path, body string, code int, out any) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != code {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if out != nil {
			if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}
	until := func(ready func() bool) {
		t.Helper()
		limit := time.NewTimer(8 * time.Second)
		defer limit.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for !ready() {
			select {
			case <-limit.C:
				t.Fatal("mock flow timed out")
			case <-tick.C:
			}
		}
	}
	call("POST", "/v1/inbox/104/library/scan", "", 202, nil)
	var library shrink.Library
	until(func() bool {
		call("GET", "/v1/inbox/104/library", "", 200, &library)
		return !library.Scanning && !library.ScannedAt.IsZero()
	})
	request := `{"vmid":130,"paths":["anime/Sample show (2026)/Season 01/episode.mkv"]}`
	var checks []arr.ShrinkCheckResult
	call("POST", "/v1/apps/media/shrink-check", request, 200, &checks)
	if len(checks) != 1 || checks[0].State != "ok" {
		t.Fatal(checks)
	}
	os.WriteFile(statePath("shrink-size-format"), nil, 0600)
	call("POST", "/v1/apps/media/shrink-check", request, 200, &checks)
	if checks[0].State != "refused" {
		t.Fatal(checks)
	}
	os.Remove(statePath("shrink-size-format"))
	call("POST", "/v1/inbox/104/shrink", `{"paths":["anime/Sample show (2026)"],"profile":"standard"}`, 400, nil)
	var jobs []shrink.Job
	call("POST", "/v1/inbox/104/shrink", `{"paths":["anime/Sample show (2026)"],"profile":"standard","confirm":"replace-originals"}`, 202, &jobs)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	call("PUT", "/v1/inbox/104/shrink/settings", `{"paused":false}`, 204, nil)
	var view shrink.View
	until(func() bool {
		call("GET", "/v1/inbox/104/shrink", "", 200, &view)
		return len(view.Jobs) == 1 && view.Jobs[0].State == "done"
	})
	if view.Jobs[0].SavedBytes != 360000000 {
		t.Fatal(view)
	}
	var rescans []arr.RescanResult
	call("POST", "/v1/apps/media/rescan", request, 200, &rescans)
	if len(rescans) != 1 {
		t.Fatal(rescans)
	}
	call("POST", "/v1/inbox/104/shrink/rescanned/"+jobs[0].ID, "", 204, nil)
	call("GET", "/v1/inbox/104/shrink", "", 200, &view)
	if !view.Jobs[0].Rescanned {
		t.Fatal(view)
	}
}
