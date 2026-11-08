package shrink

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func assertUpgradeAndRecoveryCopies(t *testing.T, s *Service, p string, j Job) {
	t.Helper()
	view := s.Status()
	if !view.Paused || view.Jobs[0].State != "failed" || len(s.state.Swaps) != 1 || !strings.Contains(view.Jobs[0].Error, unix.EEXIST.Error()) {
		t.Fatal("collision did not pause for recovery", view)
	}
	data, err := os.ReadFile(filepath.Join(s.Root, p))
	if err != nil || string(data) != "Arr upgrade" {
		t.Fatal("lost the Arr upgrade", string(data), err)
	}
	restoredSize(t, s, "anime/.iglo-shrink/"+backupName(j.ID), 400000000)
	restoredSize(t, s, "anime/.iglo-shrink/"+tempName(j.ID), 40000000)
}

func TestInstallNeverReplacesAnArrUpgrade(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	if err := s.Settings(false); err != nil {
		t.Fatal(err)
	}
	s.checkpoint = func(point string) {
		if point == "original-backed-up" {
			if err := os.WriteFile(filepath.Join(s.Root, p), []byte("Arr upgrade"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.step(context.Background())
	assertUpgradeAndRecoveryCopies(t, s, p, j)
	// Restart must retain the collision and all three copies too.
	recovered, err := New(s.Dir, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.Status().Paused || len(recovered.state.Swaps) != 1 {
		t.Fatal("restart discarded the collision", recovered.Status())
	}
	data, err := os.ReadFile(filepath.Join(s.Root, p))
	if err != nil || string(data) != "Arr upgrade" {
		t.Fatal("restart lost the Arr upgrade", err)
	}
	restoredSize(t, recovered, "anime/.iglo-shrink/"+backupName(j.ID), 400000000)
	restoredSize(t, recovered, "anime/.iglo-shrink/"+tempName(j.ID), 40000000)
}

func TestRestoreNeverReplacesAnArrUpgrade(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		name := "rollback"
		if recovery {
			name = "startup"
		}
		t.Run(name, func(t *testing.T) {
			s, p := fixture(t)
			j := submit(t, s, p)
			if err := s.Settings(false); err != nil {
				t.Fatal(err)
			}
			raced := false
			rename := func(from int, old string, to int, name string, noReplace bool) error {
				if old == tempName(j.ID) {
					return unix.EIO
				}
				if old == backupName(j.ID) {
					raced = true
					if !noReplace {
						t.Fatal("restore did not request exclusion")
					}
					// Insert after recovery's missing-file check, immediately
					// before the restore syscall.
					if err := os.WriteFile(filepath.Join(s.Root, p), []byte("Arr upgrade"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return renameFile(from, old, to, name, noReplace)
			}
			if recovery {
				crashJob(t, s, "original-backed-up")
				recovered, err := newService(s.Dir, s.Root, func(v *Service) { v.rename = rename })
				if err != nil {
					t.Fatal(err)
				}
				s = recovered
			} else {
				s.rename = rename
				s.step(context.Background())
			}
			if !raced {
				t.Fatal("restore was not reached")
			}
			assertUpgradeAndRecoveryCopies(t, s, p, j)
		})
	}
}

func TestRenameNoReplace(t *testing.T) {
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	for _, name := range []string{"source", "target"} {
		if err := os.WriteFile(filepath.Join(dir.Name(), name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := renameNoReplace(int(dir.Fd()), "source", int(dir.Fd()), "target"); !errors.Is(err, unix.EEXIST) {
		t.Fatalf("expected EEXIST, got %v", err)
	}
	for _, name := range []string{"source", "target"} {
		data, err := os.ReadFile(filepath.Join(dir.Name(), name))
		if err != nil || string(data) != name {
			t.Fatalf("collision changed %s: %q %v", name, data, err)
		}
	}
	if err := renameNoReplace(int(dir.Fd()), "source", int(dir.Fd()), "free"); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir.Name(), "free"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil || string(data) != "source" {
		t.Fatal("free target did not receive source", string(data), err)
	}
	if _, err := os.Lstat(filepath.Join(dir.Name(), "source")); !os.IsNotExist(err) {
		t.Fatal("successful rename kept source", err)
	}
}
