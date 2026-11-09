package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"iglo/internal/agent"
	"iglo/internal/apps"
	"iglo/internal/auth"
	"iglo/internal/inbox"
	"iglo/internal/proxmox"
	"iglo/internal/server"
	"iglo/internal/shrink"
	"iglo/internal/stacks/arr"
)

// Exercise the real manager, worker and mock handlers without opening ports.
type shrinkManagerAgent struct {
	apps.Agent
	h             http.Handler
	worker        *shrink.Service
	mu            sync.Mutex
	rescans, acks int
}

func (a *shrinkManagerAgent) call(ctx context.Context, method, path string, in, out any) error {
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return err
		}
	}
	w := httptest.NewRecorder()
	a.h.ServeHTTP(w, httptest.NewRequest(method, path, &body).WithContext(ctx))
	if w.Code >= 300 {
		return fmt.Errorf("mock %s: %s", path, w.Body)
	}
	if out != nil {
		return json.Unmarshal(w.Body.Bytes(), out)
	}
	return nil
}
func (a *shrinkManagerAgent) Library(ctx context.Context, id int) (shrink.Library, error) {
	var v shrink.Library
	err := a.call(ctx, "GET", fmt.Sprintf("/v1/inbox/%d/library", id), nil, &v)
	return v, err
}
func (a *shrinkManagerAgent) ScanLibrary(ctx context.Context, id int) error {
	return a.call(ctx, "POST", fmt.Sprintf("/v1/inbox/%d/library/scan", id), nil, nil)
}
func (a *shrinkManagerAgent) ShrinkJobs(ctx context.Context, id int) (shrink.View, error) {
	var v shrink.View
	err := a.call(ctx, "GET", fmt.Sprintf("/v1/inbox/%d/shrink", id), nil, &v)
	return v, err
}
func (a *shrinkManagerAgent) SubmitShrink(ctx context.Context, id int, in shrink.Input) ([]shrink.Job, error) {
	var v []shrink.Job
	err := a.call(ctx, "POST", fmt.Sprintf("/v1/inbox/%d/shrink", id), in, &v)
	return v, err
}
func (a *shrinkManagerAgent) ShrinkSettings(ctx context.Context, id int, paused bool) error {
	return a.call(ctx, "PUT", fmt.Sprintf("/v1/inbox/%d/shrink/settings", id), shrink.Settings{Paused: &paused}, nil)
}
func (a *shrinkManagerAgent) CancelShrink(ctx context.Context, id int, job string) error {
	return a.call(ctx, "POST", fmt.Sprintf("/v1/inbox/%d/shrink/cancel/%s", id, job), nil, nil)
}
func (a *shrinkManagerAgent) ShrinkRescanned(ctx context.Context, id int, job string) error {
	if err := a.call(ctx, "POST", fmt.Sprintf("/v1/inbox/%d/shrink/rescanned/%s", id, job), nil, nil); err != nil {
		return err
	}
	a.mu.Lock()
	a.acks++
	a.mu.Unlock()
	return nil
}
func (a *shrinkManagerAgent) ShrinkCheck(ctx context.Context, in agent.MediaLibraryRequest) ([]arr.ShrinkCheckResult, error) {
	var v []arr.ShrinkCheckResult
	err := a.call(ctx, "POST", "/v1/apps/media/shrink-check", in, &v)
	return v, err
}
func (a *shrinkManagerAgent) RescanLibrary(ctx context.Context, in agent.MediaLibraryRequest) ([]arr.RescanResult, error) {
	var v []arr.RescanResult
	err := a.call(ctx, "POST", "/v1/apps/media/rescan", in, &v)
	a.mu.Lock()
	a.rescans++
	a.mu.Unlock()
	return v, err
}
func (a *shrinkManagerAgent) InboxEvents(ctx context.Context, _ int, on func(string)) error {
	events, stop := a.worker.Subscribe()
	defer stop()
	on("")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case topic := <-events:
			on(topic)
		}
	}
}

type shrinkManagerProxmox struct{ apps.Proxmox }

func (shrinkManagerProxmox) Resources(context.Context) ([]proxmox.Resource, error) {
	return []proxmox.Resource{{Type: "lxc", VMID: 104, Tags: "iglo;inbox", Status: "running"}, {Type: "lxc", VMID: 130, Tags: "iglo;media", Status: "running"}}, nil
}

type shrinkManagerJellyfin struct {
	server.Jellyfin
	h         http.Handler
	mu        sync.Mutex
	refreshes int
}

func (j *shrinkManagerJellyfin) RefreshLibrary(ctx context.Context) error {
	w := httptest.NewRecorder()
	j.h.ServeHTTP(w, httptest.NewRequest("POST", "/Library/Refresh", nil).WithContext(ctx))
	if w.Code != 204 {
		return fmt.Errorf("refresh: %d", w.Code)
	}
	j.mu.Lock()
	j.refreshes++
	j.mu.Unlock()
	return nil
}
func TestLibraryShrinkThroughManager(t *testing.T) {
	t.Chdir(t.TempDir())
	admission := t.TempDir()
	if err := os.WriteFile(filepath.Join(admission, "admit-until"), []byte("-1"), 0600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	worker, err := addShrinkRoutes(mux, &inbox.Service{AdmissionDir: admission})
	if err != nil {
		t.Fatal(err)
	}
	a := &shrinkManagerAgent{h: mux, worker: worker}
	j := &shrinkManagerJellyfin{h: newJellyfin()}
	service := apps.New(apps.Options{Agent: a, Proxmox: shrinkManagerProxmox{}, Logger: slog.New(slog.DiscardHandler)})

	// A session created through the auth service exercises the real middleware.
	hash, err := auth.HashPassword("password")
	if err != nil {
		t.Fatal(err)
	}
	authService := auth.NewService(auth.Admin{Username: "admin", PasswordHash: hash, TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"})
	token, err := authService.Login("admin", "password", totp("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", time.Now()), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	h := server.New(server.Options{Apps: service, Jellyfin: j, Auth: authService, Logger: slog.New(slog.DiscardHandler), Web: fstest.MapFS{"index.html": {Data: []byte("app")}}})
	ctx, cancel := context.WithCancel(t.Context())
	var wg sync.WaitGroup
	wg.Go(func() { worker.Run(ctx) })
	wg.Go(func() { service.WatchInbox(ctx) })
	defer func() { cancel(); wg.Wait() }()
	call := func(method, path, body string, code int, out any) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "iglo_session", Value: token})
		r.Header.Set("X-Iglo-Request", "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if out != nil {
			if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}
	until := func(ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(45 * time.Second)
		for !ready() {
			if time.Now().After(deadline) {
				t.Fatal("manager flow timed out")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	var library shrink.Library
	until(func() bool {
		call("GET", "/api/inbox/library", "", 200, &library)
		return !library.Scanning && !library.ScannedAt.IsZero()
	})
	if len(library.Kinds) != 4 {
		t.Fatal(library)
	}
	statuses := map[string]bool{}
	var visit func(shrink.Folder)
	visit = func(f shrink.Folder) {
		for _, file := range f.Files {
			statuses[file.Status] = true
			if file.HDR {
				statuses["hdr"] = true
			}
			if strings.HasSuffix(file.Path, ".mp4") {
				statuses["mp4"] = true
			}
		}
		for _, child := range f.Folders {
			visit(child)
		}
	}
	for _, kind := range library.Kinds {
		visit(kind)
	}
	for _, status := range []string{"ready", "small", "skipped", "hdr", "mp4"} {
		if !statuses[status] {
			t.Fatalf("missing fixture %s: %v", status, statuses)
		}
	}
	// A failed media check must leave the worker queue empty.
	if err := os.WriteFile(statePath("shrink-check-error"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/inbox/shrink", `{"paths":["anime/Sample show (2026)/Season 01/episode.mkv"],"profile":"standard","confirm":"replace-originals"}`, 502, nil)
	var untouched shrink.View
	call("GET", "/api/inbox/shrink", "", 200, &untouched)
	if len(untouched.Jobs) != 0 {
		t.Fatal("failed guard submitted jobs", untouched)
	}
	if err := os.Remove(statePath("shrink-check-error")); err != nil {
		t.Fatal(err)
	}
	var submission apps.ShrinkSubmission
	call("POST", "/api/inbox/shrink", `{"paths":["anime/Sample show (2026)/Season 01/episode.mkv","anime/Size scored title/Season 01/episode.mkv","movies/Not smaller/episode.mkv"],"profile":"standard","confirm":"replace-originals"}`, 202, &submission)
	if len(submission.Jobs) != 2 || len(submission.Refused) != 1 {
		t.Fatal(submission)
	}
	call("PUT", "/api/inbox/shrink/settings", `{"paused":false}`, 204, nil)
	var queue shrink.View
	until(func() bool {
		call("GET", "/api/inbox/shrink", "", 200, &queue)
		done, small := false, false
		for _, job := range queue.Jobs {
			done = done || (job.State == "done" && job.Rescanned)
			small = small || job.State == "not-smaller"
		}
		return done && small
	})
	a.mu.Lock()
	rescans, acks := a.rescans, a.acks
	a.mu.Unlock()
	j.mu.Lock()
	refreshes := j.refreshes
	j.mu.Unlock()
	if rescans != 1 || acks != 1 || refreshes != 1 {
		t.Fatal(rescans, acks, refreshes)
	}
}
