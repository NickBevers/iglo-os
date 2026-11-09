package shrink

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func pendingRecovery(t *testing.T) (*Service, string, Job) {
	t.Helper()
	s, p := fixture(t)
	j := submit(t, s, p)
	if err := s.Settings(false); err != nil {
		t.Fatal(err)
	}
	s.rename = func(from int, old string, to int, name string, noReplace bool) error {
		if noReplace {
			return unix.EIO
		}
		return renameFile(from, old, to, name, noReplace)
	}
	s.step(context.Background())
	if !s.Status().Paused || len(s.state.Swaps) != 1 {
		t.Fatal("expected pending restore", s.Status())
	}
	return s, p, j
}

func TestSubmitRefusesPendingRecovery(t *testing.T) {
	for _, state := range []string{"failed", "done"} {
		t.Run(state, func(t *testing.T) {
			s, p, _ := pendingRecovery(t)
			s.state.Jobs[0].State = state
			other := "anime/Other/Season 01/b.mkv"
			writeSource(t, s.Root, other)
			if err := s.Scan(context.Background()); err != nil {
				t.Fatal(err)
			}
			// The library name is absent. Recovery must still get the clear error,
			// even with another ready file in the same request.
			jobs, err := s.Submit(Input{Paths: []string{other, p}, Profile: "standard", Confirm: "replace-originals"})
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), p+" has pending replacement recovery") || len(jobs) != 0 || len(s.state.Jobs) != 1 {
				t.Fatalf("pending path was not refused atomically: %v %v", jobs, err)
			}
			// Other files remain eligible. An intent is a per-path refusal.
			submit(t, s, other)
		})
	}
}

func TestSubmitRechecksRecoveryAfterFilesystemChecks(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.state.Jobs[0].State = "failed"
	// A replacement commits after the initial pending-path check.
	s.MountOK = func() bool {
		s.mu.Lock()
		s.state.Swaps[j.ID] = swapIntent{}
		s.mu.Unlock()
		return true
	}
	jobs, err := s.Submit(Input{Paths: []string{p}, Profile: "standard", Confirm: "replace-originals"})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "pending replacement recovery") || len(jobs) != 0 || len(s.state.Jobs) != 1 {
		t.Fatalf("pending intent raced submission: %v %v", jobs, err)
	}
}

func TestPendingRecoveryRetriesAfterMountReturns(t *testing.T) {
	s, p, _ := pendingRecovery(t)
	start := s.lastRecoveryAttempt
	checks := 0
	mounted := false
	s.MountOK = func() bool { checks++; return mounted }
	s.rename = nil
	s.Inspect = func(context.Context, *os.File) (Probe, error) {
		t.Fatal("recovery must not probe or scan the library")
		return Probe{}, nil
	}
	s.Encode = func(context.Context, *os.File, string, Probe, string, string, func(float64)) error {
		t.Fatal("recovery must not encode again")
		return nil
	}
	s.retryRecoveries(start.Add(recoveryMountCheckEvery))
	if checks != 1 || len(s.state.Swaps) != 1 || !s.lastRecoveryAttempt.Equal(start) {
		t.Fatal("attempted recovery while the mount was down")
	}
	mounted = true
	s.retryRecoveries(start.Add(recoveryMountCheckEvery + time.Second))
	if checks != 1 {
		t.Fatal("mount check was not bounded")
	}
	titles := s.retryRecoveries(start.Add(2 * recoveryMountCheckEvery))
	view := s.Status()
	if checks != 2 || len(s.state.Swaps) != 0 || view.Jobs[0].State != "done" || !view.Paused {
		t.Fatal("mount return did not finish recovery while keeping the queue paused", view)
	}
	if len(titles) != 1 || titles[0] != titlePath(p) {
		t.Fatal("recovered title was not returned for a single refresh", titles)
	}
	restoredSize(t, s, p, 40000000)
	assertNoTemp(t, s)
	s.retryRecoveries(start.Add(time.Hour))
	if checks != 2 {
		t.Fatal("idle recovery did a mount check")
	}
}

func TestPendingRecoveryRetriesAtMostEveryTenMinutes(t *testing.T) {
	s, _, j := pendingRecovery(t)
	start := s.lastRecoveryAttempt
	checks, attempts := 0, 0
	s.MountOK = func() bool { checks++; return true }
	s.rename = func(int, string, int, string, bool) error { attempts++; return unix.EIO }
	for i := 1; i <= 19; i++ {
		s.retryRecoveries(start.Add(time.Duration(i) * recoveryMountCheckEvery))
	}
	if checks != 19 || attempts != 0 {
		t.Fatalf("premature retry: mount checks %d, attempts %d", checks, attempts)
	}
	s.retryRecoveries(start.Add(recoveryRetryEvery))
	if attempts != 1 || len(s.state.Swaps) != 1 {
		t.Fatal("ten minute retry did not run once")
	}
	s.retryRecoveries(start.Add(recoveryRetryEvery + recoveryMountCheckEvery))
	if attempts != 1 {
		t.Fatal("failed recovery retried immediately")
	}
	for _, name := range []string{tempName(j.ID), backupName(j.ID)} {
		if _, err := os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", name)); err != nil {
			t.Fatal("retry lost a copy", err)
		}
	}
	s.rename = nil
	s.retryRecoveries(start.Add(2 * recoveryRetryEvery))
	if len(s.state.Swaps) != 0 || s.Status().Jobs[0].State != "done" {
		t.Fatal("later periodic recovery did not finish", s.Status())
	}
}

func TestRunRetriesPendingRecovery(t *testing.T) {
	s, _, _ := pendingRecovery(t)
	s.rename = nil
	// Put Run's first check at a mount-return transition. The paused queue must
	// recover without a restart or a new encode.
	s.nextRecoveryCheck = time.Time{}
	s.recoveryMounted = false
	s.recoveryMountKnown = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.checkpoint = func(point string) {
		if point == "swap-cleared" {
			cancel()
		}
	}
	s.Run(ctx)
	if len(s.state.Swaps) != 0 || s.Status().Jobs[0].State != "done" {
		t.Fatal("Run did not retry a pending recovery", s.Status())
	}
}
