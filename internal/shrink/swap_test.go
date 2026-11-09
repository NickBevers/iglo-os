package shrink

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

type simulatedCrash string

func crashAt(t *testing.T, point string, run func()) {
	t.Helper()
	defer func() {
		v := recover()
		if v == nil {
			t.Fatalf("did not crash at %s", point)
		}
		if v != simulatedCrash(point) {
			panic(v)
		}
	}()
	run()
}
func crashJob(t *testing.T, s *Service, point string) {
	t.Helper()
	s.checkpoint = func(actual string) {
		if actual == point {
			panic(simulatedCrash(point))
		}
	}
	crashAt(t, point, func() { s.step(context.Background()) })
}
func restoredSize(t *testing.T, s *Service, p string, want int64) {
	t.Helper()
	info, err := os.Stat(filepath.Join(s.Root, p))
	if err != nil || info.Size() != want {
		t.Fatalf("%s size: %v %v; want %d", p, info, err, want)
	}
	f, err := os.Open(filepath.Join(s.Root, p))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	marker := "original library source"
	if want == 40000000 {
		marker = "encoded library result"
	}
	data := make([]byte, len(marker))
	if _, err = f.ReadAt(data, 0); err != nil || string(data) != marker {
		t.Fatalf("wrong recovery content for %s: %q %v", p, data, err)
	}

}
func TestSwapCrashRecoveryAtEveryStep(t *testing.T) {
	for _, point := range []string{"temp-created", "encoded", "result-closed", "result-verified", "swap-recorded", "original-backed-up", "result-installed", "result-confirmed", "done-recorded", "backup-removed", "swap-cleared"} {
		t.Run(point, func(t *testing.T) {
			s, p := fixture(t)
			j := submit(t, s, p)
			if err := s.Settings(false); err != nil {
				t.Fatal(err)
			}
			crashJob(t, s, point)
			logs := []string{}
			recovered, err := newService(s.Dir, s.Root, func(v *Service) {
				v.logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
			})
			if err != nil {
				t.Fatal(err)
			}
			preCommit := point == "temp-created" || point == "encoded" || point == "result-closed" || point == "result-verified"
			wantState := "done"
			wantSize := int64(40000000)
			if preCommit {
				wantState = "queued"
				wantSize = 400000000
			}
			if job := recovered.Status().Jobs[0]; job.State != wantState || job.Rescanned {
				t.Fatal(job)
			}
			restoredSize(t, recovered, p, wantSize)
			if len(recovered.state.Swaps) != 0 {
				t.Fatal("intent not retired")
			}
			assertNoTemp(t, recovered)
			if point != "swap-cleared" && len(logs) == 0 {
				t.Fatal("recovery did not log its action")
			}
			again, err := New(s.Dir, s.Root)
			if err != nil || again.Status().Jobs[0].State != wantState {
				t.Fatal("recovery is not idempotent", err)
			}
			_ = j
		})
	}
}
func TestRecoveryCanCrashAndRestartAgain(t *testing.T) {
	for _, point := range []string{"backup-restored", "original-backed-up", "result-installed", "result-confirmed", "done-recorded", "backup-removed", "swap-cleared"} {
		t.Run(point, func(t *testing.T) {
			s, p := fixture(t)
			submit(t, s, p)
			s.Settings(false)
			crashJob(t, s, "original-backed-up")
			crashAt(t, point, func() {
				_, _ = newService(s.Dir, s.Root, func(v *Service) {
					v.checkpoint = func(actual string) {
						if actual == point {
							panic(simulatedCrash(point))
						}
					}
				})
			})
			recovered, err := New(s.Dir, s.Root)
			if err != nil {
				t.Fatal(err)
			}
			if recovered.Status().Jobs[0].State != "done" {
				t.Fatal(recovered.Status())
			}
			restoredSize(t, recovered, p, 40000000)
			assertNoTemp(t, recovered)
		})
	}
}
func TestSwapRenameFailuresAndLostReplies(t *testing.T) {
	for _, mode := range []string{"backup-fails", "install-fails", "restore-fails", "backup-reply-lost", "install-reply-lost", "original-lost"} {
		t.Run(mode, func(t *testing.T) {
			s, p := fixture(t)
			j := submit(t, s, p)
			s.Settings(false)
			s.rename = func(from int, old string, to int, name string, noReplace bool) error {
				switch {
				case old == filepath.Base(p) && mode == "backup-fails":
					return unix.EXDEV
				case old == filepath.Base(p) && mode == "original-lost":
					if err := unix.Unlinkat(from, old, 0); err != nil {
						t.Fatal(err)
					}
					return unix.EIO
				case old == tempName(j.ID) && (mode == "install-fails" || mode == "restore-fails"):
					return unix.EXDEV
				case old == backupName(j.ID) && mode == "restore-fails":
					return unix.EIO
				}
				if err := renameFile(from, old, to, name, noReplace); err != nil {
					return err
				}
				if (old == filepath.Base(p) && mode == "backup-reply-lost") || (old == tempName(j.ID) && mode == "install-reply-lost") {
					return unix.EIO
				}
				return nil
			}
			s.step(context.Background())
			job := s.Status().Jobs[0]
			switch mode {
			case "restore-fails":
				if job.State != "failed" || !s.Status().Paused || len(s.state.Swaps) != 1 {
					t.Fatal(job)
				}
				for _, name := range []string{tempName(j.ID), backupName(j.ID)} {
					if _, err := os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", name)); err != nil {
						t.Fatal("lost a recovery copy", err)
					}
				}
				recovered, err := New(s.Dir, s.Root)
				if err != nil {
					t.Fatal(err)
				}
				if recovered.Status().Jobs[0].State != "done" {
					t.Fatal(recovered.Status())
				}
				restoredSize(t, recovered, p, 40000000)
				assertNoTemp(t, recovered)
			case "original-lost":
				if !s.Status().Paused || len(s.state.Swaps) != 1 {
					t.Fatal(s.Status())
				}
				info, err := os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", tempName(j.ID)))
				if err != nil || info.Size() != 40000000 {
					t.Fatal("deleted the only remaining copy", info, err)
				}
				recovered, err := New(s.Dir, s.Root)
				if err != nil || !recovered.Status().Paused {
					t.Fatal("unsafe recovery", err)
				}
				if _, err := os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", tempName(j.ID))); err != nil {
					t.Fatal(err)
				}
			case "install-reply-lost":
				if job.State != "done" {
					t.Fatal(job)
				}
				restoredSize(t, s, p, 40000000)
				assertNoTemp(t, s)
			default:
				if job.State != "failed" {
					t.Fatal(job)
				}
				restoredSize(t, s, p, 400000000)
				assertNoTemp(t, s)
			}
		})
	}
}
func TestStateWriteFailuresAtSwapBoundaries(t *testing.T) {
	for _, point := range []string{"result-verified", "swap-recorded", "backup-removed"} {
		t.Run(point, func(t *testing.T) {
			s, p := fixture(t)
			submit(t, s, p)
			s.Settings(false)
			s.checkpoint = func(actual string) {
				if actual == point {
					s.saveFault = func() error { return errors.New("injected state write failure") }
				}
			}
			s.step(context.Background())
			if !s.Status().Paused {
				t.Fatal("did not pause after failed persistence")
			}
			recovered, err := New(s.Dir, s.Root)
			if err != nil {
				t.Fatal(err)
			}
			want := "done"
			size := int64(40000000)
			if point == "result-verified" {
				want = "queued"
				size = 400000000
			}
			if recovered.Status().Jobs[0].State != want {
				t.Fatal(recovered.Status())
			}
			restoredSize(t, recovered, p, size)
			assertNoTemp(t, recovered)
		})
	}
}
func TestSwapClosesMediaHandlesAndUsesKindStaging(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.Settings(false)
	inspect := s.Inspect
	handles := []*os.File{}
	s.Inspect = func(ctx context.Context, f *os.File) (Probe, error) {
		handles = append(handles, f)
		return inspect(ctx, f)
	}
	encode := s.Encode
	s.Encode = func(ctx context.Context, f *os.File, out string, probe Probe, kind, profile string, progress func(float64)) error {
		if filepath.Dir(out) != filepath.Join(s.Root, "anime/.iglo-shrink") {
			t.Fatal("staging crosses the unit's writable mount", out)
		}
		return encode(ctx, f, out, probe, kind, profile, progress)
	}
	s.rename = func(from int, old string, to int, name string, noReplace bool) error {
		for _, f := range handles {
			if _, err := f.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("media handle still open during %s -> %s: %v", old, name, err)
			}
		}
		return renameFile(from, old, to, name, noReplace)
	}
	s.step(context.Background())
	if s.Status().Jobs[0].State != "done" {
		t.Fatal(s.Status())
	}
	assertNoTemp(t, s)
	// Neither scans nor submission may see the hidden backups or outputs.
	writeSource(t, s.Root, "anime/.iglo-shrink/hidden.mkv")
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	for path := range s.files {
		if strings.Contains(path, ".iglo-shrink") {
			t.Fatal(path)
		}
	}
	if _, err := s.Submit(Input{Paths: []string{"anime/.iglo-shrink/hidden.mkv"}, Profile: "standard", Confirm: "replace-originals"}); err == nil {
		t.Fatal("hidden staging accepted")
	}
	_ = j
}
func TestCancelAfterCommitCannotChangeDone(t *testing.T) {
	for _, point := range []string{"swap-recorded", "result-installed"} {
		t.Run(point, func(t *testing.T) {
			s, p := fixture(t)
			j := submit(t, s, p)
			s.Settings(false)
			result := make(chan error, 1)
			s.checkpoint = func(actual string) {
				if actual == point {
					go func() { result <- s.Cancel(j.ID) }()
				}
			}
			s.step(context.Background())
			if err := <-result; err == nil {
				t.Fatal("cancelled after replacement began")
			}
			if s.Status().Jobs[0].State != "done" {
				t.Fatal(s.Status())
			}
			restoredSize(t, s, p, 40000000)
		})
	}
}
func TestFailedResultVerificationRetainsBackup(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.Settings(false)
	s.rename = func(from int, old string, to int, name string, noReplace bool) error {
		if err := renameFile(from, old, to, name, noReplace); err != nil {
			return err
		}
		if old == tempName(j.ID) {
			return os.Truncate(filepath.Join(s.Root, p), 17)
		}
		return nil
	}
	s.step(context.Background())
	if s.Status().Jobs[0].State == "done" || !s.Status().Paused {
		t.Fatal(s.Status())
	}
	info, err := os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", backupName(j.ID)))
	if err != nil || info.Size() != 400000000 {
		t.Fatal("lost the backup", info, err)
	}
	recovered, err := New(s.Dir, s.Root)
	if err != nil || !recovered.Status().Paused {
		t.Fatal(err)
	}
	info, err = os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", backupName(j.ID)))
	if err != nil || info.Size() != 400000000 {
		t.Fatal("recovery deleted the backup", err)
	}
}
func TestRecoveryRestoreFailureKeepsBothFiles(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.Settings(false)
	crashJob(t, s, "original-backed-up")
	recovered, err := newService(s.Dir, s.Root, func(v *Service) { v.rename = func(int, string, int, string, bool) error { return unix.EIO } })
	if err != nil || !recovered.Status().Paused {
		t.Fatal(err)
	}
	for _, name := range []string{tempName(j.ID), backupName(j.ID)} {
		if _, err := os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", name)); err != nil {
			t.Fatal(err)
		}
	}
	final, err := New(s.Dir, s.Root)
	if err != nil || final.Status().Jobs[0].State != "done" {
		t.Fatal(err)
	}
	restoredSize(t, final, p, 40000000)
}

func TestBackupCleanupFailurePreservesAcknowledgement(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.Settings(false)
	s.remove = func(fd int, name string) error {
		if name == backupName(j.ID) {
			return unix.EIO
		}
		return unix.Unlinkat(fd, name, 0)
	}
	s.step(context.Background())
	if s.Status().Jobs[0].State != "done" || !s.Status().Paused || !strings.Contains(s.Status().Jobs[0].Error, "recovery") {
		t.Fatal(s.Status())
	}
	if _, err := os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", backupName(j.ID))); err != nil {
		t.Fatal("lost backup", err)
	}
	if err := s.Rescanned(j.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(s.Dir, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	if job := recovered.Status().Jobs[0]; job.State != "done" || !job.Rescanned {
		t.Fatal(job)
	}
	restoredSize(t, recovered, p, 40000000)
	assertNoTemp(t, recovered)
}
func TestRecoveryMissingResultRestoresOriginal(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.Settings(false)
	crashJob(t, s, "original-backed-up")
	if err := os.Remove(filepath.Join(s.Root, "anime/.iglo-shrink", tempName(j.ID))); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(s.Dir, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	restoredSize(t, recovered, p, 400000000)
	assertNoTemp(t, recovered)
	if job := recovered.Status().Jobs[0]; job.State != "failed" || !recovered.Status().Paused {
		t.Fatal(job)
	}
}
func TestRecoveryPreservesAnExternalUpgrade(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.Settings(false)
	crashJob(t, s, "original-backed-up")
	writeSource(t, s.Root, p)
	os.Truncate(filepath.Join(s.Root, p), 400000001)
	recovered, err := New(s.Dir, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	restoredSize(t, recovered, p, 400000001)
	if !recovered.Status().Paused || len(recovered.state.Swaps) != 1 {
		t.Fatal(recovered.Status())
	}
	for _, name := range []string{tempName(j.ID), backupName(j.ID)} {
		if _, err := os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", name)); err != nil {
			t.Fatal("overwrote a recovery copy", err)
		}
	}
}
