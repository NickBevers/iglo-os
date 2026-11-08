package shrink

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type identityInfo struct {
	size     int64
	modified time.Time
	inode    uint64
	unknown  bool
}

func (i identityInfo) Name() string       { return "video.mkv" }
func (i identityInfo) Size() int64        { return i.size }
func (i identityInfo) Mode() fs.FileMode  { return 0664 }
func (i identityInfo) ModTime() time.Time { return i.modified }
func (i identityInfo) IsDir() bool        { return false }
func (i identityInfo) Sys() any {
	if i.unknown {
		return nil
	}
	return &syscall.Stat_t{Ino: i.inode}
}

func TestFileIdentityMatchesCIFSRename(t *testing.T) {
	original := identityInfo{size: 5000000, modified: time.Unix(1794156348, 271528600), inode: 271207}
	for _, tc := range []struct {
		name                                 string
		shift                                time.Duration
		inode                                uint64
		size                                 int64
		unknownExpected, unknownActual, want bool
	}{
		{name: "rename shifts mtime by 0.5 ms", shift: 515200 * time.Nanosecond, inode: 271207, size: 5000000, want: true},
		{name: "inode changed", inode: 271208, size: 5000000, want: false},
		{name: "size changed", inode: 271207, size: 5000001, want: false},
		{name: "mtime five seconds later", shift: 5 * time.Second, inode: 271207, size: 5000000, want: false},
		{name: "mtime five seconds earlier", shift: -5 * time.Second, inode: 271207, size: 5000000, want: false},
		{name: "unknown observed inode", shift: 500 * time.Microsecond, size: 5000000, unknownActual: true, want: true},
		{name: "unknown recorded inode", shift: 500 * time.Microsecond, inode: 271208, size: 5000000, unknownExpected: true, want: true},
		{name: "both inodes zero", shift: 500 * time.Microsecond, size: 5000000, unknownExpected: true, want: true},
		{name: "unknown inode still checks size", size: 5000001, unknownActual: true, want: false},
		{name: "unknown inode still checks mtime", shift: 5 * time.Second, size: 5000000, unknownActual: true, want: false},
		{name: "positive boundary", shift: 2 * time.Second, inode: 271207, size: 5000000, want: true},
		{name: "negative boundary", shift: -2 * time.Second, inode: 271207, size: 5000000, want: true},
		{name: "outside boundary", shift: 2*time.Second + time.Nanosecond, inode: 271207, size: 5000000, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := original
			if tc.unknownExpected {
				before.inode = 0
			}
			after := identityInfo{size: tc.size, modified: before.modified.Add(tc.shift), inode: tc.inode, unknown: tc.unknownActual}
			expected := identifyFile(before)
			if got := expected.matches(after); got != tc.want {
				t.Fatalf("identity matches: %v; want %v", got, tc.want)
			}
			if got := !SourceChanged(before, after); got != tc.want {
				t.Fatalf("source matches: %v; want %v", got, tc.want)
			}
			j := Job{ID: "test", SourceBytes: before.size, Modified: before.modified}
			s := &Service{state: state{Sources: map[string]fileIdentity{j.ID: expected}, Swaps: map[string]swapIntent{}}}
			if got := s.originalMatches(after, j); got != tc.want {
				t.Fatalf("job source matches: %v; want %v", got, tc.want)
			}
			intent := swapIntent{Original: expected, Result: cached{Bytes: expected.Bytes, Inode: expected.Inode, Modified: expected.Modified}}
			s.state.Swaps[j.ID] = intent
			if got := s.originalMatches(after, j); got != tc.want {
				t.Fatalf("backup matches: %v; want %v", got, tc.want)
			}
		})
	}
	if identifyFile(original).matches(nil) {
		t.Fatal("missing file matched")
	}
}

// Model the measured NAS behaviour after each successful rename, including
// restore and replay. Futimes keeps this helper portable on Linux and macOS.
func shiftRenameMtime(from int, old string, to int, name string, noReplace bool) error {
	if err := renameFile(from, old, to, name, noReplace); err != nil {
		return err
	}
	fd, err := unix.Openat(to, name, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	shifted := unix.NsecToTimeval(info.ModTime().Add(500 * time.Microsecond).UnixNano())
	return unix.Futimes(fd, []unix.Timeval{shifted, shifted})
}

func TestSwapAndRestartTolerateRenamedFileMtime(t *testing.T) {
	for _, point := range []string{"", "original-backed-up", "result-installed", "done-recorded"} {
		t.Run("crash_"+point, func(t *testing.T) {
			s, p := fixture(t)
			j := submit(t, s, p)
			s.Settings(false)
			s.rename = shiftRenameMtime
			original, err := os.Stat(filepath.Join(s.Root, p))
			if err != nil {
				t.Fatal(err)
			}
			if id := s.state.Sources[j.ID]; id.Inode == 0 || id.Inode != fileInode(original) {
				t.Fatal("job did not record the source inode", id)
			}
			if point == "" {
				s.step(context.Background())
			} else {
				crashJob(t, s, point)
			}
			if point != "" {
				intent := s.state.Swaps[j.ID]
				if intent.Original.Inode != fileInode(original) || intent.Result.Inode == 0 {
					t.Fatal("intent did not record both inodes", intent)
				}
			}
			recovered, err := newService(s.Dir, s.Root, func(v *Service) { v.rename = shiftRenameMtime })
			if err != nil {
				t.Fatal(err)
			}
			if recovered.Status().Jobs[0].State != "done" {
				t.Fatal(recovered.Status())
			}
			restoredSize(t, recovered, p, 40000000)
			assertNoTemp(t, recovered)
			installed, err := os.Stat(filepath.Join(s.Root, p))
			if err != nil {
				t.Fatal(err)
			}
			cache := recovered.state.Cache[p]
			if cache.Inode != fileInode(installed) || !cache.Modified.Equal(installed.ModTime()) {
				t.Fatal("cache kept pre-rename metadata", cache, installed)
			}
			recovered.MountOK = func() bool { return true }
			recovered.Inspect = func(context.Context, *os.File) (Probe, error) {
				t.Fatal("reprobed the renamed result")
				return Probe{}, nil
			}
			if err = recovered.Scan(context.Background()); err != nil || recovered.files[p].Status != "done" {
				t.Fatal("lost done status", recovered.files[p], err)
			}
		})
	}
}

func TestLegacyIntentWithoutInodesRecoversShiftedFiles(t *testing.T) {
	s, p := fixture(t)
	j := submit(t, s, p)
	s.Settings(false)
	s.rename = shiftRenameMtime
	crashJob(t, s, "result-installed")
	delete(s.state.Sources, j.ID)
	intent := s.state.Swaps[j.ID]
	intent.Original = fileIdentity{}
	intent.Result.Inode = 0
	s.state.Swaps[j.ID] = intent
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

func replaceSameMetadata(t *testing.T, s *Service, p string) {
	t.Helper()
	name := filepath.Join(s.Root, p)
	original, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := os.CreateTemp(filepath.Dir(name), "upgrade-*.mkv")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(replacement.Name())
	if _, err = replacement.WriteString("Sonarr upgrade"); err == nil {
		err = replacement.Truncate(original.Size())
	}
	replacement.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(replacement.Name(), original.ModTime(), original.ModTime()); err != nil {
		t.Fatal(err)
	}
	upgraded, err := os.Stat(replacement.Name())
	if err != nil {
		t.Fatal(err)
	}
	if fileInode(original) == 0 || fileInode(upgraded) == 0 {
		t.Fatal("test filesystem supplies no inode")
	}
	if fileInode(original) == fileInode(upgraded) {
		t.Fatal("upgrade fixture reused an inode")
	}
	if err = os.Rename(replacement.Name(), name); err != nil {
		t.Fatal(err)
	}
}

func TestSameSizeSameMtimeUpgradeIsChanged(t *testing.T) {
	for _, when := range []string{"queued", "restarted", "encoding"} {
		t.Run(when, func(t *testing.T) {
			s, p := fixture(t)
			submit(t, s, p)
			if when == "encoding" {
				encode := s.Encode
				s.Encode = func(ctx context.Context, f *os.File, out string, probe Probe, kind, profile string, progress func(float64)) error {
					if err := encode(ctx, f, out, probe, kind, profile, progress); err != nil {
						return err
					}
					replaceSameMetadata(t, s, p)
					return nil
				}
			} else {
				replaceSameMetadata(t, s, p)
				if when == "restarted" {
					recovered, err := New(s.Dir, s.Root)
					if err != nil {
						t.Fatal(err)
					}
					recovered.AdmissionDir = s.AdmissionDir
					recovered.MountOK = s.MountOK
					s = recovered
				}
				s.Encode = func(context.Context, *os.File, string, Probe, string, string, func(float64)) error {
					t.Fatal("encoded an upgraded file")
					return nil
				}
			}
			s.Settings(false)
			s.step(context.Background())
			if s.Status().Jobs[0].State != "changed" {
				t.Fatal(s.Status())
			}
			file, err := os.Open(filepath.Join(s.Root, p))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			header := make([]byte, len("Sonarr upgrade"))
			if _, err = file.ReadAt(header, 0); err != nil || string(header) != "Sonarr upgrade" {
				t.Fatal("replaced the upgrade", err)
			}
			assertNoTemp(t, s)
		})
	}
}

func TestDoneCacheSurvivesLaterCIFSAttributeDrift(t *testing.T) {
	s, p := fixture(t)
	submit(t, s, p)
	s.Settings(false)
	s.rename = shiftRenameMtime
	s.step(context.Background())
	name := filepath.Join(s.Root, p)
	installed, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	later := installed.ModTime().Add(500 * time.Microsecond)
	if err = os.Chtimes(name, later, later); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(s.Dir, s.Root)
	if err != nil {
		t.Fatal(err)
	}
	recovered.MountOK = func() bool { return true }
	recovered.Inspect = func(context.Context, *os.File) (Probe, error) {
		t.Fatal("reprobed completed output after metadata drift")
		return Probe{}, nil
	}
	if err = recovered.Scan(context.Background()); err != nil || recovered.files[p].Status != "done" {
		t.Fatal(recovered.files[p], err)
	}
}

func TestProbeCacheNoticesSameMetadataNewInode(t *testing.T) {
	s, p := fixture(t)
	originalInode := s.state.Cache[p].Inode
	replaceSameMetadata(t, s, p)
	count := 0
	inspect := s.Inspect
	s.Inspect = func(ctx context.Context, file *os.File) (Probe, error) { count++; return inspect(ctx, file) }
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count != 1 || s.state.Cache[p].Inode == originalInode {
		t.Fatal("reused an upgraded file's probe", count, s.state.Cache[p].Inode)
	}
}
