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
	"testing/synctest"
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
		limit := time.NewTimer(12 * time.Second)
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
	call("POST", "/v1/inbox/104/shrink", `{"paths":["anime/Sample show (2026)/Season 01/episode.mkv"],"profile":"standard"}`, 400, nil)
	var jobs []shrink.Job
	call("POST", "/v1/inbox/104/shrink", `{"paths":["anime/Sample show (2026)/Season 01/episode.mkv"],"profile":"standard","confirm":"replace-originals"}`, 202, &jobs)
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

func TestShrinkMockThrottlesOnlyProgress(t *testing.T) {
	before := shrink.View{Jobs: []shrink.Job{{ID: "one", State: "running", Progress: 0.1}}}
	after := shrink.View{Jobs: []shrink.Job{{ID: "one", State: "running", Progress: 0.4}}}
	if mockShrinkChanged(before, after, 29*time.Second) {
		t.Fatal("progress notified early")
	}
	if !mockShrinkChanged(before, after, 30*time.Second) {
		t.Fatal("progress was not notified")
	}
	if before.Jobs[0].Progress != 0.1 || after.Jobs[0].Progress != 0.4 {
		t.Fatal("comparison changed a snapshot")
	}
	after.Jobs[0].State = "done"
	if !mockShrinkChanged(before, after, time.Second) {
		t.Fatal("final state was throttled")
	}
	before = after
	after = shrink.View{Paused: true, Jobs: append([]shrink.Job{}, before.Jobs...)}
	if !mockShrinkChanged(before, after, time.Second) {
		t.Fatal("pause was throttled")
	}
	if mockShrinkChanged(after, after, time.Hour) {
		t.Fatal("idle queue notified")
	}
	before = after
	after.Admitted = true
	if !mockShrinkChanged(before, after, time.Second) {
		t.Fatal("admission change was throttled")
	}
}

func TestShrinkMockAdmissionBecomesAvailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		t.Chdir(t.TempDir())
		mux := http.NewServeMux()
		if _, err := addShrinkRoutes(mux, &inbox.Service{AdmissionDir: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
		admitted := func() bool {
			t.Helper()
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", "/v1/inbox/104/shrink", nil))
			var view struct {
				Admitted *bool `json:"admitted"`
			}
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
			if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Admitted == nil {
				t.Fatal(w.Body, err)
			}
			return *view.Admitted
		}
		if admitted() {
			t.Fatal("mock admission was not initially held")
		}
		time.Sleep(mockShrinkAdmissionDelay - time.Nanosecond)
		if admitted() {
			t.Fatal("mock admitted work early")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if !admitted() {
			t.Fatal("mock admission did not become available")
		}
	})
}
