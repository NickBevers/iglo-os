package shrink

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOnlyExplicitFilesAreQueued(t *testing.T) {
	s, p := fixture(t)
	writeSource(t, s.Root, "anime/Show/Season 01/unseen.mkv")
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"anime/Show", "anime/Show/Season 01"} {
		if _, err := s.Submit(Input{Paths: []string{folder, p}, Profile: "standard", Confirm: "replace-originals"}); err == nil {
			t.Fatal("accepted folder", folder)
		}
		if len(s.Status().Jobs) != 0 {
			t.Fatal("partial submission")
		}
	}
	os.MkdirAll(filepath.Join(s.Root, "anime/not-a-file.mkv"), 0700)
	if _, err := s.Submit(Input{Paths: []string{"anime/not-a-file.mkv"}, Profile: "standard", Confirm: "replace-originals"}); err == nil {
		t.Fatal("accepted a directory named .mkv")
	}
	jobs, err := s.Submit(Input{Paths: []string{p}, Profile: "standard", Confirm: "replace-originals"})
	if err != nil || len(jobs) != 1 || jobs[0].Path != p {
		t.Fatal(jobs, err)
	}
}
func TestSubmissionLimitsAndUnacknowledgedHistory(t *testing.T) {
	s, p := fixture(t)
	paths := make([]string, 101)
	for i := range paths {
		paths[i] = p
	}
	input := Input{Paths: paths, Profile: "standard", Confirm: "replace-originals"}
	if _, err := s.Submit(input); err == nil || !strings.Contains(err.Error(), "100 file paths") || !strings.Contains(err.Error(), "no files were queued") {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		s.state.Jobs = append(s.state.Jobs, Job{ID: fmt.Sprintf("%032x", i+1), Path: fmt.Sprintf("anime/Old/%d.mkv", i), Profile: "standard", State: "done", SourceBytes: 400000000, Modified: time.Now()})
	}
	if _, err := s.Submit(Input{Paths: []string{p}, Profile: "standard", Confirm: "replace-originals"}); err == nil || !strings.Contains(err.Error(), "100-job limit") || !strings.Contains(err.Error(), "room for 0 of 1") || !strings.Contains(err.Error(), "no files were queued") {
		t.Fatal(err)
	}
	if len(s.Status().Jobs) != 100 {
		t.Fatal("evicted unacknowledged work")
	}
	s.state.Jobs[0].Rescanned = true
	jobs, err := s.Submit(Input{Paths: []string{p}, Profile: "standard", Confirm: "replace-originals"})
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	for _, job := range s.Status().Jobs {
		if job.ID == fmt.Sprintf("%032x", 1) {
			t.Fatal("acknowledged slot not evicted")
		}
	}
	s.state.Swaps[s.state.Jobs[0].ID] = swapIntent{}
	s.state.Jobs[0].State = "failed"
	if evictable(s.state.Jobs[0], s.state.Swaps) {
		t.Fatal("evicted a pending recovery")
	}
	// Keep HTTP limits visible in the error body, without changing the response schema.
	body, _ := json.Marshal(input)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/shrink", strings.NewReader(string(body))))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "100 file paths") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestProgressEventsAreLimitedToThirtySeconds(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.state.Jobs[0].State = "running"
	events, stop := s.Subscribe()
	defer stop()
	start := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	last := start
	for i := 1; i <= 61; i++ {
		s.recordProgress(j.ID, float64(i)/100, start.Add(time.Duration(i)*time.Second), &last)
	}
	count := 0
	for len(events) > 0 {
		if name := <-events; name != "shrink" {
			t.Fatal(name)
		}
		count++
	}
	if count != 2 || s.Status().Jobs[0].Progress != 0.61 {
		t.Fatal("progress or event rate", count, s.Status())
	}
	if err := s.Settings(false); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatal("settings change was throttled")
	}
	<-events
	if err := s.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatal("terminal change was throttled")
	}
}
func TestVideoDurationAndChapterVerification(t *testing.T) {
	src, out := sourceProbe(), resultProbe()
	out.Streams[0].Duration = "1300" // Container duration is still identical.
	if Verify(src, out, "anime") == nil {
		t.Fatal("used container duration instead of video")
	}
	for _, tc := range []struct {
		source, result string
		ok             bool
	}{{"100", "101.1", true}, {"100", "101.101", false}, {"1440", "1442.44", true}, {"1440", "1442.441", false}, {"7200", "7203", true}, {"7200", "7203.01", false}} {
		src, out = sourceProbe(), resultProbe()
		src.Streams[0].Duration = tc.source
		out.Streams[0].Duration = tc.result
		if got := Verify(src, out, "anime") == nil; got != tc.ok {
			t.Fatalf("duration %s -> %s: %v", tc.source, tc.result, got)
		}
	}
	src, out = sourceProbe(), resultProbe()
	src.Chapters = []json.RawMessage{json.RawMessage(`{"id":1}`)}
	if Verify(src, out, "anime") == nil {
		t.Fatal("dropped chapters")
	}
	out.Chapters = src.Chapters
	if err := Verify(src, out, "anime"); err != nil {
		t.Fatal(err)
	}
	src.Streams[0].Duration = ""
	src.Streams[0].Tags = map[string]string{"DURATION": "00:24:00.000000000"}
	if src.VideoDuration() != 1440 {
		t.Fatal(src.VideoDuration())
	}
	delete(src.Streams[0].Tags, "DURATION")
	if Verify(src, out, "anime") == nil {
		t.Fatal("accepted unknown video duration")
	}
}
func TestCoversAndUnsupportedStreams(t *testing.T) {
	cover := Stream{Index: 7, Type: "video", Codec: "mjpeg", Width: 100, Height: 200, Disposition: map[string]int{"attached_pic": 1}}
	src, out := sourceProbe(), resultProbe()
	src.Streams = append(src.Streams, cover)
	out.Streams = append(out.Streams, cover)
	if err := Verify(src, out, "anime"); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(Arguments(src, "anime", "standard", "in", "out", true), " ")
	if !strings.Contains(args, "-map 0:7") || !strings.Contains(args, "-c:v copy") || !strings.Contains(args, "-c:v:0 hevc_qsv") || !strings.Contains(args, "-disposition:v:1 attached_pic") {
		t.Fatal(args)
	}
	out.Streams = out.Streams[:len(out.Streams)-1]
	if Verify(src, out, "anime") == nil {
		t.Fatal("cover loss accepted")
	}
	for _, extra := range []Stream{{Type: "video", Codec: "h264"}, {Type: "data", Codec: "bin_data"}} {
		s, p := fixture(t)
		probe := sourceProbe()
		probe.Streams = append(probe.Streams, extra)
		s.state.Cache = map[string]cached{}
		s.Inspect = func(context.Context, *os.File) (Probe, error) { return probe, nil }
		if err := s.Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
		if s.files[p].Status != "skipped" || s.files[p].Reason == "" {
			t.Fatal(s.files[p])
		}
		jobs, err := s.Submit(Input{Paths: []string{p}, Profile: "standard", Confirm: "replace-originals"})
		if err != nil || len(jobs) != 0 {
			t.Fatal(jobs, err)
		}
	}
}
func TestStatisticsAreClearedOnlyOnEncodedStreams(t *testing.T) {
	p := sourceProbe()
	tags := map[string]string{"BPS": "9", "BPS-eng": "9", "NUMBER_OF_BYTES": "9", "NUMBER_OF_FRAMES-eng": "9", "DURATION": "00:24:00", "_STATISTICS_WRITING_APP": "mkvmerge", "_STATISTICS_TAGS": "BPS", "title": "Keep"}
	for i := range p.Streams {
		p.Streams[i].Tags = tags
	}
	args := strings.Join(Arguments(p, "movies", "standard", "in", "out", false), " ")
	for _, tag := range []string{"BPS", "BPS-eng", "NUMBER_OF_BYTES", "NUMBER_OF_FRAMES-eng", "DURATION", "_STATISTICS_WRITING_APP", "_STATISTICS_TAGS"} {
		for _, spec := range []string{"v:0", "a:0"} {
			if !strings.Contains(args, "-metadata:s:"+spec+" "+tag+"=") {
				t.Fatal("stale tag", args)
			}
		}
	}
	for _, bad := range []string{"-metadata:s:a:1 BPS=", "-metadata:s:s:0 DURATION=", "title="} {
		if strings.Contains(args, bad) {
			t.Fatal("cleared metadata on a copied stream", args)
		}
	}
}
func TestRealFFmpegClearsStatistics(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}
	dir := t.TempDir()
	src, out := filepath.Join(dir, "source.mkv"), filepath.Join(dir, "result.mkv")
	cmd := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=64x64:r=10:d=1", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:v", "libx264", "-c:a", "flac", "-metadata:s:v:0", "BPS=99999999", "-metadata:s:a:0", "NUMBER_OF_BYTES=999999999", "-metadata:s:a:0", "title=Keep track title", src)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate fixture: %v %s", err, data)
	}
	probe := func(path string) Probe {
		t.Helper()
		data, err := exec.Command(ffprobe, "-v", "error", "-show_streams", "-show_format", "-show_chapters", "-of", "json", path).Output()
		if err != nil {
			t.Fatal(err)
		}
		var p Probe
		if err = json.Unmarshal(data, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	before := probe(src)
	if before.Streams[0].Tags["BPS"] != "99999999" {
		t.Fatal("fixture lacks stale tags")
	}
	cmd = exec.Command(ffmpeg, Arguments(before, "anime", "standard", src, out, false)...)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("encode fixture: %v %s", err, data)
	}
	after := probe(out)
	if err = Verify(before, after, "anime"); err != nil {
		t.Fatal(err)
	}
	if after.Streams[0].Tags["BPS"] == "99999999" || after.Streams[1].Tags["NUMBER_OF_BYTES"] == "999999999" {
		t.Fatal("copied stale statistics")
	}
	if after.Streams[1].Tags["title"] != "Keep track title" {
		t.Fatal("lost ordinary stream metadata")
	}
}
func TestUnitKeepsEachKindOnOneWritableMount(t *testing.T) {
	data, err := os.ReadFile("../../deploy/stacks/inbox/iglo-inbox.service")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ReadWritePaths=") {
			if strings.Contains(line, ".iglo-shrink") {
				t.Fatal("staging has its own mount", line)
			}
			for _, kind := range LibraryFolders {
				if !strings.Contains(line, "-/data/videos/"+kind) {
					t.Fatal("missing optional kind", line)
				}
			}
		}
	}
	data, err = os.ReadFile("../../deploy/stacks/inbox/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `/data/videos/$folder/.iglo-shrink`) {
		t.Fatal("installer does not create staging inside each kind")
	}
}

func TestMissingKindsAndCustomNamesDoNotBreakStartup(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	writeSource(t, root, "custom-anime/Show/episode.mkv")
	s, err := New(t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	s.MountOK = func() bool { return true }
	s.Inspect = func(context.Context, *os.File) (Probe, error) { t.Fatal("probed a custom folder"); return Probe{}, nil }
	if err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.files) != 0 || len(s.Library().Kinds) != len(LibraryFolders) {
		t.Fatal(s.Library())
	}
	if _, err = s.Submit(Input{Paths: []string{"custom-anime/Show/episode.mkv"}, Profile: "standard", Confirm: "replace-originals"}); err == nil {
		t.Fatal("silently adopted custom folder settings")
	}
}

func TestLegacyStagingSymlinkIsRetainedWithoutFollowingIt(t *testing.T) {
	s, _ := fixture(t)
	target := filepath.Join(t.TempDir(), "missing")
	if err := os.Symlink(target, filepath.Join(s.Root, ".iglo-shrink")); err != nil {
		t.Fatal(err)
	}
	logged := false
	if _, err := newService(s.Dir, s.Root, func(v *Service) {
		v.logf = func(format string, _ ...any) {
			if strings.Contains(format, "without following") {
				logged = true
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if !logged {
		t.Fatal("did not identify the legacy symlink")
	}
	if _, err := os.Lstat(filepath.Join(s.Root, ".iglo-shrink")); err != nil {
		t.Fatal("deleted legacy symlink", err)
	}
}
