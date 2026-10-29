package inbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T) *Service {
	t.Helper()
	s, err := New(t.TempDir(), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.MountOK = func() bool { return true }
	s.FreeBytes = func() (uint64, error) { return 1 << 40, nil }
	s.Execute = func(_ context.Context, source, stage, profile string) (string, error) {
		p := filepath.Join(stage, "output.mkv")
		data, err := os.ReadFile(source)
		if err != nil {
			return "", err
		}
		return p, os.WriteFile(p, data, 0600)
	}
	os.MkdirAll(filepath.Join(s.Dir, "admission"), 0700)
	os.WriteFile(filepath.Join(s.Dir, "admission", "admit-until"), []byte("-1"), 0600)
	s.Settings(false, "preserve")
	return s
}
func source(t *testing.T, s *Service, name string) {
	t.Helper()
	p := filepath.Join(s.Source, name)
	os.MkdirAll(filepath.Dir(p), 0755)
	if err := os.WriteFile(p, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	os.Chtimes(p, old, old)
}
func TestInboxDurablePublicationAndNoAutomaticDuplicates(t *testing.T) {
	s := fixture(t)
	source(t, s, "family/clip.mp4")
	if added, err := s.Scan(); err != nil || added != 1 {
		t.Fatal(added, err)
	}
	if !s.step(context.Background()) {
		t.Fatal("did not process")
	}
	status := s.Status()
	if len(status.Jobs) != 1 || status.Jobs[0].State != "succeeded" {
		t.Fatal(status)
	}
	out := filepath.Join(s.Output, status.Jobs[0].File)
	if data, err := os.ReadFile(out); err != nil || string(data) != "source" {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(s.Source, "family/clip.mp4")); string(data) != "source" {
		t.Fatal("source changed")
	}
	s.mu.Lock()
	s.state.Jobs = nil
	s.save()
	s.mu.Unlock()
	reloaded, err := New(s.Dir, s.Source, s.Output)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.MountOK = func() bool { return true }
	if added, err := reloaded.Scan(); err != nil || added != 0 {
		t.Fatal("history eviction repeated a completed source", added, err)
	}
}
func TestInboxAdmissionPauseAndLowDisk(t *testing.T) {
	s := fixture(t)
	source(t, s, "clip.mp4")
	s.Submit(Input{Path: "clip.mp4", Profile: "preserve"})
	os.Remove(filepath.Join(s.Dir, "admission", "admit-until"))
	if s.step(context.Background()) {
		t.Fatal("ignored missing lease")
	}
	os.WriteFile(filepath.Join(s.Dir, "admission", "admit-until"), []byte("-1"), 0600)
	s.Settings(true, "preserve")
	if s.step(context.Background()) {
		t.Fatal("ignored pause")
	}
	s.Settings(false, "preserve")
	s.FreeBytes = func() (uint64, error) { return 1, nil }
	s.step(context.Background())
	if !strings.Contains(s.Status().Jobs[0].Error, "SSD") {
		t.Fatal(s.Status())
	}
	files, _ := os.ReadDir(s.Output)
	if len(files) != 0 {
		t.Fatal("published with insufficient staging")
	}
}
func TestInboxRefusesPathsSymlinksAndUnsettledFiles(t *testing.T) {
	s := fixture(t)
	source(t, s, "clip.mp4")
	for _, path := range []string{"../clip.mp4", "/tmp/clip.mp4", ".hidden.mp4", "clip.m3u8", "clip\\name.mp4"} {
		if _, err := s.Submit(Input{Path: path, Profile: "preserve"}); !errors.Is(err, ErrInvalid) {
			t.Fatal(path, err)
		}
	}
	os.Symlink(filepath.Join(s.Source, "clip.mp4"), filepath.Join(s.Source, "link.mp4"))
	if _, err := s.Submit(Input{Path: "link.mp4", Profile: "preserve"}); err == nil {
		t.Fatal("followed source link")
	}
	os.WriteFile(filepath.Join(s.Source, "new.mp4"), []byte("uploading"), 0600)
	if _, err := s.Submit(Input{Path: "new.mp4", Profile: "preserve"}); err == nil {
		t.Fatal("processed unfinished upload")
	}
}
func TestInboxFailureAndNASLossNeverPublish(t *testing.T) {
	for _, lost := range []bool{false, true} {
		s := fixture(t)
		source(t, s, "clip.mp4")
		s.Submit(Input{Path: "clip.mp4", Profile: "preserve"})
		s.Execute = func(_ context.Context, _, stage, _ string) (string, error) {
			path := filepath.Join(stage, "partial.mp4")
			os.WriteFile(path, []byte("partial"), 0600)
			if lost {
				s.MountOK = func() bool { return false }
				return path, nil
			}
			return path, errors.New("conversion failed")
		}
		s.step(context.Background())
		if s.Status().Jobs[0].State != "failed" {
			t.Fatal(s.Status())
		}
		files, _ := os.ReadDir(s.Output)
		if len(files) != 0 {
			t.Fatal("published failed job")
		}
	}
}
func lease(t *testing.T, s *Service, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.AdmissionDir, "admit-until"), []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestInboxTimedScansFollowAdmission(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	future := strconv.FormatInt(now.Add(time.Minute).Unix(), 10)
	for _, c := range []struct {
		name, lease string
		paused      bool
		since       time.Duration
		want        bool
	}{
		{"held lease, never scanned", "0", false, 0, false},
		{"held lease, long ago", "0", false, 24 * time.Hour, false},
		{"lease, never scanned", future, false, 0, true},
		{"lease, 10 minutes ago", future, false, 10 * time.Minute, false},
		{"lease, 30 minutes ago", future, false, 30 * time.Minute, true},
		{"lease while paused", future, true, 0, false},
		{"native, never scanned", "-1", false, 0, true},
		{"native, 5 hours ago", "-1", false, 5 * time.Hour, false},
		{"native, 6 hours ago", "-1", false, 6 * time.Hour, true},
		{"native while paused", "-1", true, 6 * time.Hour, false},
	} {
		lease(t, s, c.lease)
		s.mu.Lock()
		s.state.Paused = c.paused
		s.lastScan = time.Time{}
		if c.since > 0 {
			s.lastScan = now.Add(-c.since)
		}
		s.mu.Unlock()
		if got := s.scanDue(now); got != c.want {
			t.Error(c.name, got)
		}
	}
}
func TestInboxRunScansOnlyOnceWorkMayStart(t *testing.T) {
	s := fixture(t)
	source(t, s, "clip.mp4")
	lease(t, s, "0")
	var mounts atomic.Int32
	s.MountOK = func() bool { mounts.Add(1); return true }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	time.Sleep(200 * time.Millisecond)
	s.mu.Lock()
	last := s.lastScan
	s.mu.Unlock()
	if !last.IsZero() || mounts.Load() != 0 {
		t.Fatal("scanned at startup while admission was held")
	}
	lease(t, s, strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
	s.wake <- struct{}{}
	deadline := time.Now().Add(5 * time.Second)
	for {
		jobs := s.Status().Jobs
		if len(jobs) == 1 && jobs[0].State == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no scan once work could start", jobs)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Another wake inside 30 minutes must not list the share again.
	s.mu.Lock()
	last = s.lastScan
	s.mu.Unlock()
	s.wake <- struct{}{}
	time.Sleep(100 * time.Millisecond)
	s.mu.Lock()
	again := s.lastScan
	s.mu.Unlock()
	if !again.Equal(last) {
		t.Fatal("scanned again within 30 minutes")
	}
	cancel()
	<-done
}
