package inbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
