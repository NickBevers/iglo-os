package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"iglo/internal/agent"
	"iglo/internal/inbox"
	"iglo/internal/shrink"
	"iglo/internal/stacks/arr"
)

const mockShrinkAdmissionDelay = 3 * time.Second

// Hold briefly so the admission state can be seen before simulated work runs.
func mockShrinkAdmission(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lease, err := filepath.Abs(filepath.Join(dir, "admit-until"))
	if err != nil {
		return err
	}
	if err = os.WriteFile(lease, []byte("0"), 0600); err != nil {
		return err
	}
	time.AfterFunc(mockShrinkAdmissionDelay, func() {
		if err := os.WriteFile(lease, []byte("-1"), 0600); err != nil {
			slog.Warn("mock shrink admission", "error", err)
		}
	})
	return nil
}

// These sparse files and probes simulate the pipeline. They contain no video.
func addShrinkRoutes(mux *http.ServeMux, in *inbox.Service) (*shrink.Service, error) {
	root, err := filepath.Abs(statePath("shrink-videos"))
	if err != nil {
		return nil, err
	}
	for _, kind := range shrink.LibraryFolders {
		if err = os.MkdirAll(filepath.Join(root, kind), 0700); err != nil {
			return nil, err
		}
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	samples := map[string]int64{"anime/Sample show (2026)/Season 01/episode.mkv": 400000000}
	for _, kind := range shrink.LibraryFolders {
		for _, name := range []string{"Ready title/Season 01/episode.mkv", "Already small/episode.mkv", "HDR title/hdr.mkv", "Camera video/video.mp4", "Not smaller/episode.mkv"} {
			size := int64(400000000)
			if strings.HasPrefix(name, "Already small/") {
				size = 310000000
			}
			samples[kind+"/"+name] = size
		}
	}
	samples["anime/Size scored title/Season 01/episode.mkv"] = 400000000
	for name, size := range samples {
		sample := filepath.Join(root, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(sample), 0700); err != nil {
			return nil, err
		}
		if _, err = os.Stat(sample); os.IsNotExist(err) {
			f, e := os.Create(sample)
			if e != nil {
				return nil, e
			}
			err = f.Truncate(size)
			f.Close()
			if err != nil {
				return nil, err
			}
			old := time.Now().Add(-time.Hour)
			if err = os.Chtimes(sample, old, old); err != nil {
				return nil, err
			}
		}
	}
	s, err := shrink.New(statePath("shrink-worker"), root)
	if err != nil {
		return nil, err
	}
	s.AdmissionDir = in.AdmissionDir
	if err = mockShrinkAdmission(s.AdmissionDir); err != nil {
		return nil, err
	}
	s.MountOK = func() bool { return true }
	s.Inspect = func(_ context.Context, f *os.File) (shrink.Probe, error) {
		info, err := f.Stat()
		if err != nil {
			return shrink.Probe{}, err
		}
		p := shrink.Probe{}
		p.Format.Duration = "1440"
		codec, pixel := "h264", "yuv420p"
		if info.Size() < 400000000 {
			codec, pixel = "hevc", "yuv420p10le"
		}
		transfer := ""
		if strings.Contains(f.Name(), "hdr.mkv") {
			transfer = "smpte2084"
		}
		p.Streams = []shrink.Stream{{Index: 0, Type: "video", Duration: "1440", Codec: codec, Width: 1920, Height: 1080, Pixel: pixel, Transfer: transfer}, {Index: 1, Type: "audio", Codec: "aac", Channels: 2, Tags: map[string]string{"language": "jpn"}}}
		return p, nil
	}
	s.Encode = func(ctx context.Context, source *os.File, out string, _ shrink.Probe, _, _ string, progress func(float64)) error {
		for i := 1; i <= 4; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				progress(float64(i) / 4)
			}
		}
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer f.Close()
		if strings.Contains(source.Name(), "Not smaller/") {
			return f.Truncate(380000000)
		}
		return f.Truncate(40000000)
	}
	h := s.Handler()
	for _, route := range []struct{ method, target string }{{"GET", "/library"}, {"POST", "/library/scan"}, {"GET", "/shrink"}, {"POST", "/shrink"}, {"PUT", "/shrink/settings"}, {"POST", "/shrink/cancel/{id}"}, {"POST", "/shrink/rescanned/{id}"}} {
		mux.HandleFunc(route.method+" /v1/inbox/{vmid}"+route.target, func(w http.ResponseWriter, r *http.Request) {
			clone := r.Clone(r.Context())
			u := *r.URL
			u.Path = strings.ReplaceAll(route.target, "{id}", r.PathValue("id"))
			clone.URL = &u
			h.ServeHTTP(w, clone)
		})
	}
	for _, command := range []string{"shrink-check", "rescan"} {
		mux.HandleFunc("POST /v1/apps/media/"+command, func(w http.ResponseWriter, r *http.Request) {
			var request agent.MediaLibraryRequest
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&request) != nil || request.VMID < 100 || request.VMID > 999999999 || arr.ValidateLibraryPaths(request.Paths) != nil {
				http.Error(w, "invalid library request", 400)
				return
			}
			if command == "shrink-check" {
				if _, err := os.Stat(statePath("shrink-check-error")); err == nil {
					http.Error(w, "simulated shrink check failure", 502)
					return
				}
			}
			checks := []arr.ShrinkCheckResult{}
			rescans := []arr.RescanResult{}
			seen := map[string]int{}
			for _, p := range request.Paths {
				parts := strings.Split(p, "/")
				title := strings.Join(parts[:2], "/")
				if i, ok := seen[title]; ok {
					checks[i].Paths = append(checks[i].Paths, p)
					continue
				}
				seen[title] = len(checks)
				check := arr.ShrinkCheckResult{Path: title, Paths: []string{p}, Title: parts[1], State: "ok-unmanaged", Reason: "not managed by Sonarr or Radarr"}
				if title == "anime/Sample show (2026)" {
					check.State = "ok"
					check.Reason = ""
					check.App = "sonarr"
					check.ID = 1
					check.ProfileID = 1
					if _, err := os.Stat(statePath("shrink-size-format")); err == nil {
						check.State = "refused"
						check.Reason = "quality profile scores a custom format with SizeSpecification: Sample size"
					}
					rescans = append(rescans, arr.RescanResult{App: "sonarr", ID: 1, Path: title})
				}
				if title == "anime/Size scored title" {
					check.State = "refused"
					check.App = "sonarr"
					check.ID = 2
					check.ProfileID = 2
					check.Reason = "quality profile scores a size custom format"
				}
				checks = append(checks, check)
			}
			if command == "shrink-check" {
				writeJSON(w, checks)
			} else {
				slog.Info("mock media rescan", "paths", request.Paths)
				writeJSON(w, rescans)
			}
		})
	}
	return s, nil
}

// mockShrinkChanged mirrors the worker's state notifications. Progress can
// notify once per 30 seconds. Final states, pause and acknowledgement are
// immediate, even when the last progress notification was just sent.
func mockShrinkChanged(before, after shrink.View, elapsed time.Duration) bool {
	if reflect.DeepEqual(before, after) {
		return false
	}
	before.Jobs = append([]shrink.Job{}, before.Jobs...)
	after.Jobs = append([]shrink.Job{}, after.Jobs...)
	for i := range before.Jobs {
		before.Jobs[i].Progress = 0
	}
	for i := range after.Jobs {
		after.Jobs[i].Progress = 0
	}
	return !reflect.DeepEqual(before, after) || elapsed >= 30*time.Second
}
func streamInboxChanges(w http.ResponseWriter, r *http.Request, in *inbox.Service, s *shrink.Service) {
	snapshot := func() map[string]string {
		a, _ := json.Marshal(in.Status())
		b, _ := json.Marshal(s.Library())
		return map[string]string{"inbox": string(a), "library": string(b)}
	}
	last := snapshot()
	lastShrink := s.Status()
	lastSent := time.Now()
	w.Header().Set("Content-Type", "text/event-stream")
	controller := http.NewResponseController(w)
	fmt.Fprint(w, ": open\n\n")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for controller.Flush() == nil {
		select {
		case <-r.Context().Done():
			return
		case now := <-ticker.C:
			current := snapshot()
			for _, name := range []string{"inbox", "library"} {
				if last[name] != current[name] {
					fmt.Fprintf(w, "data: %s\n\n", name)
				}
			}
			currentShrink := s.Status()
			if mockShrinkChanged(lastShrink, currentShrink, now.Sub(lastSent)) {
				fmt.Fprint(w, "data: shrink\n\n")
				lastShrink = currentShrink
				lastSent = now
			}
			last = current
			fmt.Fprint(w, ": keep-alive\n\n")
		}
	}
}
