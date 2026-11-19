package shrink

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func libraryFile(lib Library, p string) (File, bool) {
	var find func([]Folder) (File, bool)
	find = func(folders []Folder) (File, bool) {
		for _, f := range folders {
			for _, file := range f.Files {
				if file.Path == p {
					return file, true
				}
			}
			if file, ok := find(f.Folders); ok {
				return file, true
			}
		}
		return File{}, false
	}
	return find(lib.Kinds)
}

func TestAutoSettingSurvivesRestart(t *testing.T) {
	s, _ := fixture(t)
	high := "high"
	if err := s.ChangeSettings(Settings{AutoAnime: &high}); err != nil {
		t.Fatal(err)
	}
	if v := s.Status(); v.AutoAnime != "high" || !v.Paused {
		t.Fatalf("auto setting changed pause or did not apply: %+v", v)
	}
	restarted, err := New(s.Dir, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	if v := restarted.Status(); v.AutoAnime != "high" {
		t.Fatalf("auto setting lost on restart: %+v", v)
	}
	bad := "fast"
	if err := s.ChangeSettings(Settings{AutoAnime: &bad}); err == nil {
		t.Fatal("accepted an unknown profile")
	}
	if err := s.ChangeSettings(Settings{}); err == nil {
		t.Fatal("accepted empty settings")
	}
	off := ""
	if err := s.ChangeSettings(Settings{AutoAnime: &off}); err != nil || s.Status().AutoAnime != "" {
		t.Fatalf("could not turn auto off: %v", err)
	}
}

func TestAutoSubmissionIsRemembered(t *testing.T) {
	s, p := fixture(t)
	writeSource(t, s.Root, "anime/Show/Season 01/b.mkv")
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(Input{Paths: []string{p}, Profile: "high", Confirm: "replace-originals", Auto: true}); err != nil {
		t.Fatal(err)
	}
	submit(t, s, "anime/Show/Season 01/b.mkv")
	lib := s.Library()
	if f, _ := libraryFile(lib, p); !f.AutoTried {
		t.Fatal("automatic submission was not marked")
	}
	if f, _ := libraryFile(lib, "anime/Show/Season 01/b.mkv"); f.AutoTried {
		t.Fatal("manual submission was marked as automatic")
	}
	restarted, err := New(s.Dir, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	restarted.MountOK, restarted.Inspect = s.MountOK, s.Inspect
	if err := restarted.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f, _ := libraryFile(restarted.Library(), p); !f.AutoTried {
		t.Fatal("automatic record lost on restart")
	}
	// A new file at the same path, like a Sonarr upgrade, gets a fresh try.
	full := filepath.Join(s.Root, p)
	if err := os.Truncate(full, 500000000); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(full, old, old)
	if err := restarted.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f, _ := libraryFile(restarted.Library(), p); f.AutoTried {
		t.Fatal("a changed file still counts as tried")
	}
}
