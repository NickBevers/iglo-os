package shrink

import (
	"context"
	"errors"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func sourceProbe() Probe {
	p := Probe{}
	p.Format.Duration = "1440"
	p.Streams = []Stream{{Duration: "1440", Index: 0, Type: "video", Codec: "h264", Pixel: "yuv420p", Width: 1920, Height: 1080}, {Index: 1, Type: "audio", Codec: "flac", Channels: 2, Tags: map[string]string{"language": "jpn"}}, {Index: 2, Type: "audio", Codec: "aac", Channels: 6, Tags: map[string]string{"language": "eng"}}, {Index: 3, Type: "subtitle", Codec: "ass"}, {Index: 4, Type: "attachment", Codec: "ttf"}}
	return p
}
func resultProbe() Probe {
	p := sourceProbe()
	p.Streams = append(p.Streams[:2], p.Streams[3:]...)
	p.Streams[0].Codec = "hevc"
	p.Streams[0].Pixel = "yuv420p10le"
	p.Streams[1].Codec = "opus"
	return p
}
func writeSource(t *testing.T, root, p string) string {
	t.Helper()
	full := filepath.Join(root, p)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString("original library source"); err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(400000000); err != nil {
		t.Fatal(err)
	}
	f.Close()
	old := time.Now().Add(-time.Hour)
	if err = os.Chtimes(full, old, old); err != nil {
		t.Fatal(err)
	}
	return full
}
func fixture(t *testing.T) (*Service, string) {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	p := "anime/Show/Season 01/a.mkv"
	writeSource(t, root, p)
	s, err := New(t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	s.MountOK = func() bool { return true }
	s.Inspect = func(_ context.Context, f *os.File) (Probe, error) {
		info, _ := f.Stat()
		if info.Size() < 400000000 {
			return resultProbe(), nil
		}
		return sourceProbe(), nil
	}
	s.Encode = func(_ context.Context, _ *os.File, out string, _ Probe, _, _ string, progress func(float64)) error {
		progress(0.5)
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err = f.WriteString("encoded library result"); err != nil {
			return err
		}
		return f.Truncate(40000000)
	}
	os.MkdirAll(s.AdmissionDir, 0700)
	os.WriteFile(filepath.Join(s.AdmissionDir, "admit-until"), []byte("-1"), 0600)
	if err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, p
}
func submit(t *testing.T, s *Service, p string) Job {
	t.Helper()
	jobs, err := s.Submit(Input{Paths: []string{p}, Profile: "standard", Confirm: "replace-originals"})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("submit: %v %v", jobs, err)
	}
	return jobs[0]
}
func TestPathValidationAndConfirmation(t *testing.T) {
	s, p := fixture(t)
	for _, bad := range []string{"../anime/a.mkv", "/anime/a.mkv", "anime/../movies/a.mkv", "home-videos/a.mkv", "anime//a.mkv", "anime/./a.mkv", "anime/.hidden/a.mkv", "anime/a\\b.mkv", "anime/a\n.mkv"} {
		if ValidatePath(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, confirm := range []string{"", "yes", "replace"} {
		if _, err := s.Submit(Input{Paths: []string{p}, Profile: "standard", Confirm: confirm}); err == nil {
			t.Fatal("accepted missing confirmation")
		}
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(s.Root, "anime", "outside"))
	os.Symlink(filepath.Join(s.Root, p), filepath.Join(s.Root, "anime", "linked.mkv"))
	writeSource(t, s.Root, "anime/clip.mp4")
	for _, bad := range []string{"anime/outside", "anime/linked.mkv", "anime/clip.mp4"} {
		if _, err := s.Submit(Input{Paths: []string{bad}, Profile: "standard", Confirm: "replace-originals"}); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
func TestFileSelectionAndProbeCache(t *testing.T) {
	s, _ := fixture(t)
	writeSource(t, s.Root, "anime/Show/Season 01/b.mkv")
	writeSource(t, s.Root, "anime/Show/Season 02/c.mkv")
	var count atomic.Int32
	inspect := s.Inspect
	s.Inspect = func(ctx context.Context, f *os.File) (Probe, error) { count.Add(1); return inspect(ctx, f) }
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatal("did not reuse first probe")
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatal("reprobed unchanged files")
	}
	jobs, err := s.Submit(Input{Paths: []string{"anime/Show/Season 01/a.mkv", "anime/Show/Season 01/b.mkv", "anime/Show/Season 01/a.mkv"}, Profile: "high", Confirm: "replace-originals"})
	if err != nil || len(jobs) != 2 {
		t.Fatalf("file selection: %v %v", jobs, err)
	}
	jobs, err = s.Submit(Input{Paths: []string{"anime/Show/Season 01/a.mkv", "anime/Show/Season 01/b.mkv", "anime/Show/Season 02/c.mkv"}, Profile: "standard", Confirm: "replace-originals"})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("file deduplication: %v %v", jobs, err)
	}
	// A different fingerprint is probed once.
	f, _ := os.OpenFile(filepath.Join(s.Root, "anime/Show/Season 02/c.mkv"), os.O_WRONLY, 0)
	f.Truncate(400000001)
	f.Close()
	s.Scan(context.Background())
	if count.Load() != 3 {
		t.Fatal("changed file not probed")
	}
}
func TestLibraryStatuses(t *testing.T) {
	s, p := fixture(t)
	for _, tc := range []struct {
		name, status string
		change       func(*Probe)
	}{{"hdr", "skipped", func(p *Probe) { p.Streams[0].Transfer = "smpte2084" }}, {"hlg", "skipped", func(p *Probe) { p.Streams[0].Transfer = "arib-std-b67" }}, {"dv", "skipped", func(p *Probe) {
		p.Streams[0].SideData = append(p.Streams[0].SideData, struct {
			Type string `json:"side_data_type"`
		}{"DOVI configuration record"})
	}}, {"small", "small", func(p *Probe) {
		p.Streams[0].Codec = "hevc"
		p.Format.Duration = "3600"
		p.Streams[0].Duration = "3600"
	}}} {
		t.Run(tc.name, func(t *testing.T) {
			s.mu.Lock()
			s.state.Cache = map[string]cached{}
			s.mu.Unlock()
			s.Inspect = func(context.Context, *os.File) (Probe, error) {
				probe := sourceProbe()
				tc.change(&probe)
				return probe, nil
			}
			if err := s.Scan(context.Background()); err != nil {
				t.Fatal(err)
			}
			if s.files[p].Status != tc.status {
				t.Fatal(s.files[p])
			}
		})
	}
}
func TestSuccessfulReplacementAndRescanAck(t *testing.T) {
	s, p := fixture(t)
	job := submit(t, s, p)
	s.Settings(false)
	if !s.step(context.Background()) {
		t.Fatal("did not work")
	}
	got := s.Status().Jobs[0]
	if got.State != "done" || got.SavedBytes != 360000000 || got.Progress != 1 {
		t.Fatal(got)
	}
	info, _ := os.Stat(filepath.Join(s.Root, p))
	if info.Size() != 40000000 {
		t.Fatal("not replaced")
	}
	if s.files[p].Status != "done" {
		t.Fatal(s.files[p])
	}
	if err := s.Rescanned(job.ID); err != nil {
		t.Fatal(err)
	}
	if !s.Status().Jobs[0].Rescanned {
		t.Fatal("ack missing")
	}
	assertNoTemp(t, s)
	// Cache and done status survive a restart without another probe.
	restored, err := New(s.Dir, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	restored.MountOK = s.MountOK
	restored.Inspect = func(context.Context, *os.File) (Probe, error) { t.Fatal("cached result reprobed"); return Probe{}, nil }
	if err = restored.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if restored.files[p].Status != "done" {
		t.Fatal(restored.files[p])
	}
}
func assertNoTemp(t *testing.T, s *Service) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(s.Root, "anime", ".iglo-shrink"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary output remains: %v %v", entries, err)
	}
}
func TestFailuresLeaveOriginal(t *testing.T) {
	for _, kind := range []string{"duration", "audio", "subtitle", "attachment", "video", "not-smaller", "changed", "encode"} {
		t.Run(kind, func(t *testing.T) {
			s, p := fixture(t)
			submit(t, s, p)
			encode := s.Encode
			s.Encode = func(ctx context.Context, f *os.File, out string, probe Probe, k, profile string, progress func(float64)) error {
				if err := encode(ctx, f, out, probe, k, profile, progress); err != nil {
					return err
				}
				if kind == "encode" {
					return errors.New("failed")
				}
				if kind == "not-smaller" {
					return os.Truncate(out, 390000000)
				}
				if kind == "changed" {
					return os.Truncate(filepath.Join(s.Root, p), 400000001)
				}
				return nil
			}
			inspect := s.Inspect
			s.Inspect = func(ctx context.Context, f *os.File) (Probe, error) {
				probe, err := inspect(ctx, f)
				if strings.Contains(f.Name(), ".iglo-shrink/") {
					switch kind {
					case "duration":
						probe.Format.Duration = "1300"
						probe.Streams[0].Duration = "1300"
					case "audio":
						probe.Streams[1].Channels = 8
					case "subtitle":
						probe.Streams = probe.Streams[:2]
					case "attachment":
						probe.Streams = probe.Streams[:3]
					case "video":
						probe.Streams = append(probe.Streams, probe.Streams[0])
					}
				}
				return probe, err
			}
			s.Settings(false)
			s.step(context.Background())
			got := s.Status().Jobs[0]
			want := "failed"
			if kind == "not-smaller" || kind == "changed" {
				want = kind
			}
			if got.State != want {
				t.Fatal(got)
			}
			info, _ := os.Stat(filepath.Join(s.Root, p))
			if info.Size() < 400000000 {
				t.Fatal("original replaced")
			}
			assertNoTemp(t, s)
		})
	}
}
func TestPauseCancelAndHoldRemoveTemp(t *testing.T) {
	for _, action := range []string{"pause", "cancel", "hold"} {
		t.Run(action, func(t *testing.T) {
			s, p := fixture(t)
			j := submit(t, s, p)
			started := make(chan struct{})
			s.Encode = func(ctx context.Context, _ *os.File, out string, _ Probe, _, _ string, _ func(float64)) error {
				os.WriteFile(out, []byte("partial"), 0600)
				close(started)
				<-ctx.Done()
				return ctx.Err()
			}
			s.Settings(false)
			done := make(chan struct{})
			go func() { s.step(context.Background()); close(done) }()
			<-started
			switch action {
			case "pause":
				s.Settings(true)
			case "cancel":
				s.Cancel(j.ID)
			case "hold":
				os.WriteFile(filepath.Join(s.AdmissionDir, "admit-until"), []byte("0"), 0600)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("encode did not stop")
			}
			want := "queued"
			if action == "cancel" {
				want = "cancelled"
			}
			if s.Status().Jobs[0].State != want {
				t.Fatal(s.Status())
			}
			assertNoTemp(t, s)
			info, _ := os.Stat(filepath.Join(s.Root, p))
			if info.Size() != 400000000 {
				t.Fatal("original replaced")
			}
		})
	}
}
func TestSourceChangedCheck(t *testing.T) {
	s, p := fixture(t)
	a, _ := os.Stat(filepath.Join(s.Root, p))
	if SourceChanged(a, a) {
		t.Fatal("same file changed")
	}
	os.Truncate(filepath.Join(s.Root, p), 400000001)
	b, _ := os.Stat(filepath.Join(s.Root, p))
	if !SourceChanged(a, b) {
		t.Fatal("size ignored")
	}
	os.Truncate(filepath.Join(s.Root, p), 400000000)
	now := time.Now()
	os.Chtimes(filepath.Join(s.Root, p), now, now)
	b, _ = os.Stat(filepath.Join(s.Root, p))
	if !SourceChanged(a, b) {
		t.Fatal("mtime ignored")
	}
	if !SourceChanged(a, fs.FileInfo(nil)) {
		t.Fatal("missing ignored")
	}
}
func TestHTTPContract(t *testing.T) {
	s, p := fixture(t)
	h := s.Handler()
	for _, tc := range []struct {
		method, path, body string
		code               int
	}{{"GET", "/library", "", 200}, {"GET", "/shrink", "", 200}, {"POST", "/shrink", `{"paths":["` + p + `"],"profile":"standard"}`, 400}, {"POST", "/shrink", `{"paths":["home-videos/x.mkv"],"profile":"standard","confirm":"replace-originals"}`, 400}, {"PUT", "/shrink/settings", `{}`, 400}, {"POST", "/library/scan", "", 202}} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestFileSelectionExcludesSkippedAndSmall(t *testing.T) {
	s, p := fixture(t)
	small := writeSource(t, s.Root, "anime/Show/Season 01/small.mkv")
	os.Truncate(small, 1000000)
	recent := writeSource(t, s.Root, "anime/Show/Season 01/recent.mkv")
	now := time.Now()
	os.Chtimes(recent, now, now)
	writeSource(t, s.Root, "anime/Show/Season 01/other.mp4")
	inspect := s.Inspect
	s.Inspect = func(ctx context.Context, f *os.File) (Probe, error) {
		probe, err := inspect(ctx, f)
		if strings.Contains(f.Name(), "already.mkv") {
			probe.Streams[0].Codec = "hevc"
			probe.Format.Duration = "3600"
			probe.Streams[0].Duration = "3600"
		}
		return probe, err
	}
	writeSource(t, s.Root, "anime/Show/Season 01/already.mkv")
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.Submit(Input{Paths: []string{p, "anime/Show/Season 01/small.mkv", "anime/Show/Season 01/recent.mkv", "anime/Show/Season 01/already.mkv"}, Profile: "standard", Confirm: "replace-originals"})
	if err != nil || len(jobs) != 1 || jobs[0].Path != p {
		t.Fatal(jobs, err)
	}
}
func TestUnchangedCacheDoesNotWriteState(t *testing.T) {
	s, _ := fixture(t)
	stateFile := filepath.Join(s.Dir, "queue.json")
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(stateFile, old, old); err != nil {
		t.Fatal(err)
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(stateFile)
	if err != nil || !info.ModTime().Equal(old) {
		t.Fatal("rewrote unchanged cache", info, err)
	}
}
func TestStartupPreservesUnknownAndLegacyFiles(t *testing.T) {
	s, _ := fixture(t)
	for _, dir := range []string{filepath.Join(s.Root, ".iglo-shrink"), filepath.Join(s.Root, "anime/.iglo-shrink")} {
		os.MkdirAll(dir, 0700)
		for _, name := range []string{"iglo-stale.mkv", "unrelated", "backup-unknown"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := New(s.Dir, s.Root); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(s.Root, ".iglo-shrink"), filepath.Join(s.Root, "anime/.iglo-shrink")} {
		for _, name := range []string{"iglo-stale.mkv", "unrelated", "backup-unknown"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Fatal("removed an unknown file", err)
			}
		}
	}
}

func TestMovedTitleAndLostMountDoNotReplace(t *testing.T) {
	for _, action := range []string{"move", "mount"} {
		t.Run(action, func(t *testing.T) {
			s, p := fixture(t)
			submit(t, s, p)
			encode := s.Encode
			mounted := true
			s.MountOK = func() bool { return mounted }
			s.Encode = func(ctx context.Context, f *os.File, out string, probe Probe, kind, profile string, progress func(float64)) error {
				if err := encode(ctx, f, out, probe, kind, profile, progress); err != nil {
					return err
				}
				if action == "move" {
					return os.Rename(filepath.Join(s.Root, "anime/Show"), filepath.Join(s.Root, "anime/Moved"))
				}
				mounted = false
				return nil
			}
			s.Settings(false)
			s.step(context.Background())
			job := s.Status().Jobs[0]
			want := "failed"
			check := filepath.Join(s.Root, p)
			if action == "move" {
				want = "changed"
				check = filepath.Join(s.Root, "anime/Moved/Season 01/a.mkv")
			}
			if job.State != want {
				t.Fatal(job)
			}
			info, err := os.Stat(check)
			if err != nil || info.Size() != 400000000 {
				t.Fatal("original changed", info, err)
			}
			assertNoTemp(t, s)
		})
	}
}
func TestScanDoesNotFollowKindSymlink(t *testing.T) {
	s, _ := fixture(t)
	outside := t.TempDir()
	writeSource(t, outside, "Outside/a.mkv")
	os.Symlink(outside, filepath.Join(s.Root, "movies"))
	inspect := s.Inspect
	s.Inspect = func(ctx context.Context, f *os.File) (Probe, error) {
		if strings.HasPrefix(f.Name(), "movies/") {
			t.Fatal("followed library kind symlink")
		}
		return inspect(ctx, f)
	}
	if err := s.Scan(context.Background()); err == nil {
		t.Fatal("accepted a symlink library kind")
	}
}
