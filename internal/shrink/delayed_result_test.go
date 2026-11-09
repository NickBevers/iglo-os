package shrink

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestResultIdentityDelayedWriteTime(t *testing.T) {
	base := time.Unix(1794159398, 395000000)
	for _, tc := range []struct {
		name               string
		recorded, observed uint64
		shift              time.Duration
		bytes              int64
		want               bool
	}{
		{"three seconds at close", 271885, 271885, 3 * time.Second, 5000000, true},
		{"another second at rename", 271885, 271885, 4 * time.Second, 5000000, true},
		{"known inode ignores all mtime drift", 271885, 271885, time.Hour, 5000000, true},
		{"different inode", 271885, 271886, 4 * time.Second, 5000000, false},
		{"different size", 271885, 271885, 4 * time.Second, 5000001, false},
		{"unknown recorded inode", 0, 271885, 3 * time.Second, 5000000, true},
		{"unknown observed inode", 271885, 0, 3 * time.Second, 5000000, true},
		{"both inodes unknown", 0, 0, 4 * time.Second, 5000000, true},
		{"unknown inode at boundary", 0, 0, 30 * time.Second, 5000000, true},
		{"unknown inode outside boundary", 0, 0, 30*time.Second + time.Nanosecond, 5000000, false},
		{"unknown inode sixty seconds off", 0, 271885, 60 * time.Second, 5000000, false},
		{"unknown inode sixty seconds earlier", 0, 0, -60 * time.Second, 5000000, false},
		{"unknown inode still checks size", 0, 0, 3 * time.Second, 5000001, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intent := swapIntent{Result: cached{Bytes: 5000000, Inode: tc.recorded, Modified: base}}
			info := identityInfo{size: tc.bytes, modified: base.Add(tc.shift), inode: tc.observed}
			if got := resultMatches(info, intent); got != tc.want {
				t.Fatalf("result matches: %v; want %v", got, tc.want)
			}
		})
	}
	if resultMatches(nil, swapIntent{Result: cached{Bytes: 5000000}}) {
		t.Fatal("missing result matched")
	}
	// The original and backup policy must not inherit the result's wider window.
	original := fileIdentity{Bytes: 5000000, Inode: 271885, Modified: base}
	later := identityInfo{size: 5000000, inode: 271885, modified: base.Add(3 * time.Second)}
	if original.matches(later) {
		t.Fatal("weakened original identity")
	}
}

func shiftPathTime(t *testing.T, path string, shift time.Duration) fs.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	when := info.ModTime().Add(shift)
	if err = os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestResultMetadataComesFromClosedAndInstalledPaths(t *testing.T) {
	for _, crashPoint := range []string{"", "result-installed", "done-recorded"} {
		t.Run("crash_"+crashPoint, func(t *testing.T) {
			s, p := fixture(t)
			j := submit(t, s, p)
			s.Settings(false)
			temp := filepath.Join(s.Root, "anime/.iglo-shrink", tempName(j.ID))
			name := filepath.Join(s.Root, p)
			inspect := s.Inspect
			handles := []*os.File{}
			s.Inspect = func(ctx context.Context, f *os.File) (Probe, error) {
				handles = append(handles, f)
				return inspect(ctx, f)
			}
			var closed, installed fs.FileInfo
			s.checkpoint = func(point string) {
				switch point {
				case "result-closed":
					for _, f := range handles {
						if _, err := f.Stat(); !errors.Is(err, os.ErrClosed) {
							t.Fatal("recorded result before closing a media handle", err)
						}
					}
					closed = shiftPathTime(t, temp, 3*time.Second)
				case "swap-recorded":
					if closed == nil {
						t.Fatal("did not stat after close")
					}
					intent := s.state.Swaps[j.ID]
					if !intent.Result.Modified.Equal(closed.ModTime()) || intent.Result.Inode != fileInode(closed) {
						t.Fatal("intent used open-handle attributes", intent.Result, closed)
					}
				case "result-installed":
					installed = shiftPathTime(t, name, time.Second)
				case "done-recorded":
					if installed == nil {
						t.Fatal("did not install result")
					}
					if !s.state.Cache[p].Modified.Equal(installed.ModTime()) || !s.state.Swaps[j.ID].Result.Modified.Equal(installed.ModTime()) {
						t.Fatal("done record kept pre-swap metadata")
					}
				}
				if point == crashPoint && crashPoint != "" {
					panic(simulatedCrash(point))
				}
			}
			if crashPoint != "" {
				crashAt(t, crashPoint, func() { s.step(context.Background()) })
			} else {
				s.step(context.Background())
			}
			if closed == nil || installed == nil {
				t.Fatal("did not exercise delayed write time")
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
			info, err := os.Stat(name)
			if err != nil {
				t.Fatal(err)
			}
			if !recovered.state.Cache[p].Modified.Equal(info.ModTime()) {
				t.Fatal("cache is not based on installed file")
			}
			// Samba can move write time yet again after the swap has finished.
			shiftPathTime(t, name, time.Second)
			recovered.MountOK = func() bool { return true }
			recovered.Inspect = func(context.Context, *os.File) (Probe, error) {
				t.Fatal("reprobed delayed-write result")
				return Probe{}, nil
			}
			if err = recovered.Scan(context.Background()); err != nil || recovered.files[p].Status != "done" {
				t.Fatal(recovered.files[p], err)
			}
		})
	}
}

func TestLegacyResultTimestampAllowsThirtySecondWindow(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.Settings(false)
	crashJob(t, s, "result-installed")
	intent := s.state.Swaps[j.ID]
	intent.Result.Inode = 0
	s.state.Swaps[j.ID] = intent
	shiftPathTime(t, filepath.Join(s.Root, p), 4*time.Second)
	if err := s.saveDurable(); err != nil {
		t.Fatal(err)
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
}

func TestResultSameSizeReplacementStillRefusesCleanup(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.Settings(false)
	crashJob(t, s, "result-installed")
	name := filepath.Join(s.Root, p)
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := os.CreateTemp(filepath.Dir(name), "changed-*.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if err = replacement.Truncate(info.Size()); err != nil {
		t.Fatal(err)
	}
	replacement.Close()
	os.Chtimes(replacement.Name(), info.ModTime(), info.ModTime())
	if err = os.Rename(replacement.Name(), name); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(s.Dir, s.Root)
	if err != nil || !recovered.Status().Paused {
		t.Fatal(err)
	}
	if recovered.Status().Jobs[0].State == "done" {
		t.Fatal("accepted a different result inode")
	}
	if _, err = os.Stat(filepath.Join(s.Root, "anime/.iglo-shrink", backupName(j.ID))); err != nil {
		t.Fatal("lost original backup", err)
	}
}

func TestPathStatRefusesSymlinkResults(t *testing.T) {
	// Lstat must refuse a symlink rather than opening the file it names.
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err = unix.Symlinkat("/dev/null", int(dir.Fd()), "result"); err != nil {
		t.Fatal(err)
	}
	if _, err = statAt(dir, "result"); err == nil {
		t.Fatal("accepted symlink as media")
	}
}
