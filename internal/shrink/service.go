package shrink

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"iglo/internal/worker"
)

const maxJobs = 100

type cached struct {
	Bytes    int64     `json:"bytes"`
	Inode    uint64    `json:"inode,omitempty"`
	Modified time.Time `json:"modified"`
	Probe    Probe     `json:"probe"`
	Done     bool      `json:"done"`
}
type state struct {
	Paused  bool                    `json:"paused"`
	Jobs    []Job                   `json:"jobs"`
	Cache   map[string]cached       `json:"cache"`
	Swaps   map[string]swapIntent   `json:"swaps,omitempty"`
	Sources map[string]fileIdentity `json:"sources,omitempty"`
}
type Service struct {
	Dir, Root, AdmissionDir string
	Inspect                 func(context.Context, *os.File) (Probe, error)
	Encode                  func(context.Context, *os.File, string, Probe, string, string, func(float64)) error
	MountOK                 func() bool
	// Gate is shared with Inbox conversion. Only one encode runs in the guest.
	Gate           chan struct{}
	logf           func(string, ...any)
	rename         func(int, string, int, string) error
	remove         func(int, string) error
	checkpoint     func(string)
	saveFault      func() error
	mu             sync.Mutex
	probeMu        sync.Mutex
	state          state
	library        Library
	files          map[string]File
	subs           map[chan string]struct{}
	wake, scanWake chan struct{}
	cancel         context.CancelFunc
	active         string
	scanMu         sync.Mutex
}

func New(dir, root string) (*Service, error) { return newService(dir, root, nil) }

func newService(dir, root string, configure func(*Service)) (*Service, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(root) != resolved {
		return nil, errors.New("library root must not contain symlinks")
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Service{Dir: dir, Root: root, AdmissionDir: filepath.Join(dir, "admission"), subs: map[chan string]struct{}{}, wake: make(chan struct{}, 1), scanWake: make(chan struct{}, 1), files: map[string]File{}, state: state{Paused: true, Jobs: []Job{}, Cache: map[string]cached{}}, library: Library{Kinds: []Folder{}}}
	s.logf = log.Printf
	if configure != nil {
		configure(s)
	}
	e := NewEncoder()
	s.Inspect = e.Probe
	s.Encode = e.Encode
	s.MountOK = func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		b, err := exec.CommandContext(ctx, "findmnt", "--mountpoint", root, "--noheadings", "--output", "FSTYPE").Output()
		return err == nil && topMountIsCIFS(string(b))
	}
	data, err := os.ReadFile(filepath.Join(dir, "queue.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(data) > 64<<20 {
		return nil, errors.New("shrink state too large")
	}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &s.state); err != nil {
			return nil, err
		}
	}
	if s.state.Cache == nil {
		s.state.Cache = map[string]cached{}
	}
	if len(s.state.Jobs) > maxJobs {
		return nil, errors.New("invalid shrink history")
	}
	if s.state.Swaps == nil {
		s.state.Swaps = map[string]swapIntent{}
	}
	if s.state.Sources == nil {
		s.state.Sources = map[string]fileIdentity{}
	}
	for _, j := range s.state.Jobs {
		if ValidatePath(j.Path) != nil || (j.Profile != "standard" && j.Profile != "high") || !validID(j.ID) {
			return nil, ErrInvalid
		}
	}
	for id := range s.state.Swaps {
		if !validID(id) || !slices.ContainsFunc(s.state.Jobs, func(j Job) bool { return j.ID == id }) {
			return nil, errors.New("invalid saved replacement intent")
		}
	}
	if err = s.recoverFiles(); err != nil {
		return nil, err
	}

	return s, nil
}
func validID(id string) bool { b, e := hex.DecodeString(id); return e == nil && len(b) == 16 }
func (s *Service) emit(name string) {
	for sub := range s.subs {
		select {
		case sub <- name:
		default:
		}
	}
}
func (s *Service) save() error {
	if s.saveFault != nil {
		if err := s.saveFault(); err != nil {
			return err
		}
	}
	return worker.Save(s.Dir, s.state)
}

// saveDurable also persists the queue-file rename before changing NAS names.
// Ordinary API writes retain the existing worker.Save error semantics.
func (s *Service) saveDurable() error {
	if err := s.save(); err != nil {
		return err
	}
	dir, err := os.Open(s.Dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (s *Service) Status() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return View{Paused: s.state.Paused, Admitted: worker.Admitted(s.AdmissionDir), Jobs: slices.Clone(s.state.Jobs)}
}
func (s *Service) Library() Library {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.library)
	var v Library
	json.Unmarshal(b, &v)
	return v
}
func signal(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}
func (s *Service) RequestScan() { signal(s.scanWake) }

// openDirectory pins one directory directly under the share without following
// a symlink. CIFS link counts are not used for validation.
func (s *Service) openDirectory(name string) (*os.File, error) {
	share, err := os.Open(s.Root)
	if err != nil {
		return nil, err
	}
	defer share.Close()
	fd, err := unix.Openat(int(share.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

// openKindRoot keeps recursive scans inside a pinned library kind.
func (s *Service) openKindRoot(kind string) (*os.Root, error) {
	dir, err := s.openDirectory(kind)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return os.OpenRoot(fmt.Sprintf("/dev/fd/%d", dir.Fd()))
}

// openSource rejects symlinks and retains the containing directory for replacement.
func (s *Service) openSource(p string) (parent, input *os.File, err error) {
	if err = ValidatePath(p); err != nil {
		return
	}
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		err = ErrInvalid
		return
	}
	root, e := s.openKindRoot(parts[0])
	if e != nil {
		err = e
		return
	}
	defer root.Close()
	rel := strings.Join(parts[1:], "/")
	for i := 1; i < len(parts); i++ {
		v, e := root.Lstat(strings.Join(parts[1:i+1], "/"))
		if e != nil || v.Mode()&os.ModeSymlink != 0 {
			err = ErrInvalid
			return
		}
	}
	parent, err = root.Open(filepath.Dir(rel))
	if err != nil {
		return
	}
	fd, e := unix.Openat(int(parent.Fd()), filepath.Base(rel), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		parent.Close()
		parent = nil
		err = e
		return
	}
	input = os.NewFile(uintptr(fd), p)
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		parent.Close()
		input.Close()
		return nil, nil, ErrInvalid
	}
	return
}
func (s *Service) checkSelection(p string) error {
	if ValidatePath(p) != nil {
		return ErrInvalid
	}
	root, err := os.OpenRoot(s.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	parts := strings.Split(p, "/")
	for i := range parts {
		v, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil || v.Mode()&os.ModeSymlink != 0 {
			return ErrInvalid
		}
	}
	info, err := root.Stat(p)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || strings.ToLower(filepath.Ext(p)) != ".mkv" {
		return ErrInvalid
	}
	return nil
}
func (s *Service) Scan(ctx context.Context) error { return s.scan(ctx, "") }

func (s *Service) scan(ctx context.Context, prefix string) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	s.mu.Lock()
	s.library.Scanning = true
	s.emit("library")
	s.mu.Unlock()
	files := map[string]File{}
	cache := map[string]cached{}
	if prefix != "" {
		s.mu.Lock()
		for p, f := range s.files {
			if p != prefix && !strings.HasPrefix(p, prefix+"/") {
				files[p] = f
			}
		}
		for p, c := range s.state.Cache {
			if p != prefix && !strings.HasPrefix(p, prefix+"/") {
				cache[p] = c
			}
		}
		s.mu.Unlock()
	}
	finish := func(err error) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.library.Scanning = false
		s.library.ScannedAt = time.Now()
		s.library.Error = ""
		if err != nil {
			s.library.Error = err.Error()
		} else {
			if !reflect.DeepEqual(cache, s.state.Cache) {
				old := s.state.Cache
				s.state.Cache = cache
				if saveErr := s.save(); saveErr != nil {
					s.state.Cache = old
					err = saveErr
					s.library.Error = saveErr.Error()
				}
			}
			if err == nil {
				s.files = files
				s.library.Kinds = buildTree(files)
			}
		}
		s.emit("library")
		return err
	}
	if !s.MountOK() {
		return finish(errors.New("Videos share is unavailable"))
	}
	for _, kind := range LibraryFolders {
		if prefix != "" && !strings.HasPrefix(prefix, kind+"/") {
			continue
		}
		root, err := s.openKindRoot(kind)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return finish(err)
		}
		base := "."
		if prefix != "" {
			base = strings.TrimPrefix(prefix, kind+"/")
		}
		err = fs.WalkDir(root.FS(), base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if p == base && d.IsDir() {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if !slices.Contains([]string{".mkv", ".mp4", ".mov", ".avi", ".m4v", ".webm", ".ts"}, ext) {
				return nil
			}
			rel := kind + "/" + p
			parent, f, err := s.openSource(rel)
			if err != nil {
				return nil
			}
			defer parent.Close()
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				return err
			}
			entry := File{Path: rel, Bytes: info.Size(), Modified: info.ModTime(), Status: "ready", Audio: []Audio{}}
			s.mu.Lock()
			c, ok := s.state.Cache[rel]
			s.mu.Unlock()
			matches := (fileIdentity{Bytes: c.Bytes, Inode: c.Inode, Modified: c.Modified}).matches(info)
			if c.Done {
				matches = (fileIdentity{Bytes: c.Bytes, Inode: c.Inode, Modified: c.Modified}).matchesResult(info)
			}
			if !ok || !matches || (!c.Done && !c.Modified.Equal(info.ModTime())) {
				s.probeMu.Lock()
				probe, err := s.Inspect(ctx, f)
				s.probeMu.Unlock()
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					entry.Status = "skipped"
					entry.Reason = "probe failed"
					files[rel] = entry
					return nil
				}
				after, err := f.Stat()
				if err != nil || SourceChanged(info, after) {
					return nil
				}
				c = cached{Bytes: info.Size(), Inode: fileInode(info), Modified: info.ModTime(), Probe: probe}
			}
			// Populate identities for probe caches written before inode tracking.
			if c.Inode == 0 {
				c.Inode = fileInode(info)
			}
			if c.Done {
				// Keep completed output marked done through CIFS attribute drift.
				c.Modified = info.ModTime()
			}
			cache[rel] = c
			v, err := c.Probe.Video()
			if err != nil {
				return err
			}
			entry.Duration = c.Probe.Duration()
			entry.Codec = v.Codec
			entry.Height = v.Height
			entry.BitDepth = v.Depth()
			entry.HDR = v.HDR()
			for _, a := range c.Probe.Streams {
				if a.Type == "audio" {
					entry.Audio = append(entry.Audio, Audio{Index: a.Index, Codec: a.Codec, Language: a.Tags["language"], Channels: a.Channels})
				}
			}
			entry.Estimates = Estimates{Standard: Estimate(c.Probe, kind, "standard"), High: Estimate(c.Probe, kind, "high")}
			switch {
			case ext != ".mkv":
				entry.Status = "skipped"
				entry.Reason = "not .mkv"
			case c.Probe.UnsupportedReason() != "":
				entry.Status = "skipped"
				entry.Reason = c.Probe.UnsupportedReason()
			case c.Done:
				entry.Status = "done"
			case entry.HDR:
				entry.Status = "skipped"
				entry.Reason = "HDR or Dolby Vision"
			case info.Size() < 300000000:
				entry.Status = "skipped"
				entry.Reason = "under 300 MB"
			case time.Since(info.ModTime()) < 30*time.Minute:
				entry.Status = "skipped"
				entry.Reason = "changed in the last 30 minutes"
			case (v.Codec == "hevc" || v.Codec == "av1") && float64(info.Size()) <= 1.25*float64(entry.Estimates.Standard):
				entry.Status = "small"
			}
			files[rel] = entry
			return nil
		})
		root.Close()
		if prefix != "" && os.IsNotExist(err) {
			err = nil
		}
		if err != nil {
			return finish(err)
		}
	}
	return finish(nil)
}
func buildTree(files map[string]File) []Folder {
	kinds := []Folder{}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	var add func(*Folder, []string, File)
	add = func(folder *Folder, parts []string, f File) {
		if len(parts) == 1 {
			folder.Files = append(folder.Files, f)
			return
		}
		i := slices.IndexFunc(folder.Folders, func(v Folder) bool { return v.Name == parts[0] })
		if i < 0 {
			folder.Folders = append(folder.Folders, Folder{Path: folder.Path + "/" + parts[0], Name: parts[0], Folders: []Folder{}, Files: []File{}})
			i = len(folder.Folders) - 1
		}
		add(&folder.Folders[i], parts[1:], f)
	}
	for _, kind := range LibraryFolders {
		folder := Folder{Path: kind, Name: kind, Folders: []Folder{}, Files: []File{}}
		for _, p := range paths {
			if strings.HasPrefix(p, kind+"/") {
				add(&folder, strings.Split(strings.TrimPrefix(p, kind+"/"), "/"), files[p])
			}
		}
		kinds = append(kinds, folder)
	}
	return kinds
}
func (s *Service) Submit(in Input) ([]Job, error) {
	if err := Validate(in); err != nil {
		return nil, err
	}
	if !s.MountOK() {
		return nil, errors.New("Videos share is unavailable")
	}
	for _, p := range in.Paths {
		if err := s.checkSelection(p); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	selected := map[string]bool{}
	for _, p := range in.Paths {
		for name, f := range s.files {
			if f.Status == "ready" && name == p {
				selected[name] = true
			}
		}
	}
	paths := make([]string, 0, len(selected))
	for p := range selected {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	paths = slices.DeleteFunc(paths, func(p string) bool {
		return slices.ContainsFunc(s.state.Jobs, func(j Job) bool { return j.Path == p && (j.State == "queued" || j.State == "running") })
	})
	available := maxJobs - len(s.state.Jobs)
	for _, j := range s.state.Jobs {
		if evictable(j, s.state.Swaps) {
			available++
		}
	}
	if len(paths) > available {
		return nil, fmt.Errorf("shrink queue has room for %d of %d selected files under its 100-job limit; no files were queued. Wait for active jobs and media rescans, or select fewer files", available, len(paths))
	}
	jobs := slices.Clone(s.state.Jobs)
	sources := maps.Clone(s.state.Sources)
	added := []Job{}
	for _, p := range paths {
		for len(jobs) >= maxJobs {
			i := slices.IndexFunc(jobs, func(j Job) bool { return evictable(j, s.state.Swaps) })
			if i < 0 {
				return nil, fmt.Errorf("shrink queue cannot fit %d selected files within its 100-job limit; no files were queued. Wait for active jobs and media rescans, or select fewer files", len(paths))
			}
			delete(sources, jobs[i].ID)
			jobs = slices.Delete(jobs, i, i+1)
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return nil, err
		}
		f := s.files[p]
		j := Job{ID: hex.EncodeToString(id[:]), Path: p, Profile: in.Profile, State: "queued", SourceBytes: f.Bytes, Modified: f.Modified}
		sources[j.ID] = fileIdentity{Bytes: f.Bytes, Inode: s.state.Cache[p].Inode, Modified: f.Modified}
		jobs = append(jobs, j)
		added = append(added, j)
	}
	if len(added) == 0 {
		return added, nil
	}
	old := s.state.Jobs
	oldSources := s.state.Sources
	s.state.Jobs = jobs
	s.state.Sources = sources
	if err := s.save(); err != nil {
		s.state.Jobs = old
		s.state.Sources = oldSources
		return nil, err
	}
	s.emit("shrink")
	signal(s.wake)
	return added, nil
}
func (s *Service) Settings(paused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Paused == paused {
		return nil
	}
	old := s.state.Paused
	s.state.Paused = paused
	if err := s.save(); err != nil {
		s.state.Paused = old
		return err
	}
	if paused && s.cancel != nil {
		s.cancel()
	}
	s.emit("shrink")
	signal(s.wake)
	return nil
}
func (s *Service) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, committed := s.state.Swaps[id]; committed {
		return fmt.Errorf("%w: replacement has started and cannot be cancelled", ErrInvalid)
	}
	i := slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.ID == id && (j.State == "queued" || j.State == "running") })
	if i < 0 {
		return ErrInvalid
	}
	old := s.state.Jobs[i]
	s.state.Jobs[i].State = "cancelled"
	s.state.Jobs[i].FinishedAt = time.Now()
	if err := s.save(); err != nil {
		s.state.Jobs[i] = old
		return err
	}
	if s.active == id && s.cancel != nil {
		s.cancel()
	}
	s.emit("shrink")
	return nil
}
func (s *Service) Rescanned(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.ID == id && j.State == "done" })
	if i < 0 {
		return ErrInvalid
	}
	if s.state.Jobs[i].Rescanned {
		return nil
	}
	s.state.Jobs[i].Rescanned = true
	if err := s.save(); err != nil {
		s.state.Jobs[i].Rescanned = false
		return err
	}
	s.emit("shrink")
	return nil
}
func (s *Service) Run(ctx context.Context) {
	s.RequestScan()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.scanWake:
			s.Scan(ctx)
		default:
		}
		if s.step(ctx) {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		case <-s.scanWake:
			s.Scan(ctx)
		}
	}
}
func SourceChanged(before, after fs.FileInfo) bool {
	return before == nil || !identifyFile(before).matches(after)
}
func (s *Service) step(ctx context.Context) bool {
	s.mu.Lock()
	i := slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.State == "queued" })
	if i < 0 || s.state.Paused || !worker.Admitted(s.AdmissionDir) {
		s.mu.Unlock()
		return false
	}
	job := s.state.Jobs[i]
	process, cancel := context.WithCancel(ctx)
	defer cancel()
	s.cancel = cancel
	s.active = job.ID
	s.state.Jobs[i].State = "running"
	if err := s.save(); err != nil {
		s.state.Jobs[i].State = "queued"
		s.cancel = nil
		s.active = ""
		s.mu.Unlock()
		cancel()
		return false
	}
	s.emit("shrink")
	s.mu.Unlock()
	watchDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		defer close(watchDone)
		for {
			select {
			case <-process.Done():
				return
			case <-ticker.C:
				if !worker.Admitted(s.AdmissionDir) {
					cancel()
					return
				}
			}
		}
	}()
	result, err := s.process(process, job)
	cancel()
	<-watchDone
	s.mu.Lock()
	i = slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.ID == job.ID })
	job = s.state.Jobs[i]
	s.cancel = nil
	s.active = ""
	if job.State != "cancelled" && job.State != "done" && !errors.Is(err, errSwapPending) {
		switch {
		case errors.Is(err, context.Canceled):
			job.State = "queued"
			job.Progress = 0
		case errors.Is(err, ErrNotSmaller):
			job.State = "not-smaller"
			job.ResultBytes = result
			job.Error = err.Error()
		case errors.Is(err, errChanged):
			job.State = "changed"
			job.Error = err.Error()
		case err != nil:
			job.State = "failed"
			job.Error = err.Error()
		default:
			// A successful swap records done and installed metadata itself.
			job.State = "failed"
			job.Error = "replacement did not record completion"
		}
		if job.State != "queued" {
			job.FinishedAt = time.Now()
		}
		s.state.Jobs[i] = job
	}
	if job.State != "done" {
		if err := s.save(); err != nil {
			s.state.Paused = true
		}
	}
	s.emit("shrink")
	s.mu.Unlock()
	// Refresh after each completed file so changed metadata and saved bytes show
	// at once. This includes the title containing the job.
	if job.State != "queued" && ctx.Err() == nil {
		s.scan(ctx, titlePath(job.Path))
	}
	return true
}

var errChanged = errors.New("source changed since the job was queued")

func (s *Service) process(ctx context.Context, j Job) (int64, error) {
	fail := func(err error) (int64, error) { return 0, err }
	if s.Gate != nil {
		select {
		case s.Gate <- struct{}{}:
			defer func() { <-s.Gate }()
		case <-ctx.Done():
			return fail(ctx.Err())
		}
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if !s.MountOK() {
		return fail(errors.New("Videos share is unavailable"))
	}
	parent, input, err := s.openSource(j.Path)
	if err != nil {
		return fail(err)
	}
	defer parent.Close()
	defer input.Close()
	before, err := input.Stat()
	if err != nil {
		return fail(err)
	}
	s.mu.Lock()
	sourceMatches := s.originalMatches(before, j)
	s.mu.Unlock()
	if !sourceMatches {
		return fail(errChanged)
	}
	s.probeMu.Lock()
	probe, err := s.Inspect(ctx, input)
	s.probeMu.Unlock()
	if err != nil {
		return fail(err)
	}
	video, err := probe.Video()
	if err != nil || probe.UnsupportedReason() != "" || video.HDR() || before.Size() < 300000000 || time.Since(before.ModTime()) < 30*time.Minute || strings.ToLower(filepath.Ext(j.Path)) != ".mkv" {
		return fail(errors.New("source is no longer ready"))
	}
	kind := strings.Split(j.Path, "/")[0]
	tempDir, err := s.stageDirectory(kind)
	if err != nil {
		return fail(err)
	}
	defer tempDir.Close()
	base := tempName(j.ID)
	fd, err := unix.Openat(int(tempDir.Fd()), base, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW, 0664)
	if err != nil {
		return fail(err)
	}
	unix.Close(fd)
	// Test checkpoints model abrupt process exits. Do not run destructive
	// deferred cleanup when unwinding such a crash. Real exits close descriptors.
	protectTemp := false
	defer func() {
		if crash := recover(); crash != nil {
			panic(crash)
		}
		if !protectTemp {
			// No source rename has been attempted before the commit boundary.
			if err := s.removeAt(tempDir, base); err != nil && !os.IsNotExist(err) {
				s.logf("shrink %s: could not remove uncommitted output: %v", j.ID, err)
			}
		}
	}()
	s.checkpointAt("temp-created")
	if err = unix.Unlinkat(int(tempDir.Fd()), base, 0); err != nil {
		return fail(err)
	}
	out := filepath.Join(s.Root, kind, ".iglo-shrink", base)
	input.Seek(0, 0)
	last := time.Now()
	progress := func(v float64) { s.recordProgress(j.ID, v, time.Now(), &last) }

	err = s.Encode(ctx, input, out, probe, kind, j.Profile, progress)
	s.checkpointAt("encoded")
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if err != nil {
		return fail(err)
	}
	fd, err = unix.Openat(int(tempDir.Fd()), base, unix.O_RDWR|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fail(err)
	}
	output := os.NewFile(uintptr(fd), out)
	defer output.Close()
	if err = output.Sync(); err != nil {
		return fail(err)
	}
	outInfo, err := output.Stat()
	if err == nil && !outInfo.Mode().IsRegular() {
		err = errors.New("result is not a regular file")
	}
	if err != nil {
		return fail(err)
	}
	s.probeMu.Lock()
	after, err := s.Inspect(ctx, output)
	s.probeMu.Unlock()
	if err != nil {
		return fail(err)
	}
	if err = Verify(probe, after, kind); err != nil {
		return fail(err)
	}
	if err = CheckSize(before.Size(), outInfo.Size()); err != nil {
		return outInfo.Size(), err
	}
	// Release every media-file handle before the first CIFS rename. Directory
	// handles remain pinned so replacement cannot follow a new path or symlink.
	if err = input.Close(); err != nil {
		return fail(err)
	}
	if err = output.Close(); err != nil {
		return fail(err)
	}
	s.checkpointAt("result-closed")
	// CIFS finalizes write time at close. The fallback identity must start
	// from a path stat after both the encoder and probe handles have closed.
	closedInfo, err := statAt(tempDir, base)
	if err != nil {
		return fail(err)
	}
	if closedInfo.Size() != outInfo.Size() || inodeChanged(fileInode(outInfo), fileInode(closedInfo)) {
		return fail(errors.New("verified output changed after close"))
	}
	outInfo = closedInfo
	s.checkpointAt("result-verified")
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil || s.state.Paused || !worker.Admitted(s.AdmissionDir) {
		return fail(context.Canceled)
	}
	if !s.MountOK() {
		return fail(errors.New("Videos share is unavailable"))
	}
	visibleParent, current, err := s.openSource(j.Path)
	if err != nil {
		return fail(errChanged)
	}
	currentInfo, err := current.Stat()
	current.Close()
	visibleParent.Close()
	if err != nil || SourceChanged(before, currentInfo) {
		return fail(errChanged)
	}
	intent := swapIntent{Original: identifyFile(before), Result: cached{Bytes: outInfo.Size(), Inode: fileInode(outInfo), Modified: outInfo.ModTime(), Probe: after, Done: true}}
	protectTemp = true
	s.state.Sources[j.ID] = intent.Original
	s.state.Swaps[j.ID] = intent
	if err = s.saveDurable(); err != nil {
		return fail(s.recoveryErrorLocked(j, err))
	}
	s.checkpointAt("swap-recorded")
	if err = s.finishSwapLocked(j, parent, tempDir, intent); err != nil {
		return fail(err)
	}

	return s.state.Cache[j.Path].Bytes, nil
}

const progressEventEvery = 30 * time.Second

// Polls can read current progress at any time. Events are limited to two a
// minute during an encode because each event causes a host pct exec refresh.
func (s *Service) recordProgress(id string, v float64, now time.Time, last *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	i := slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.ID == id })
	if i < 0 || s.state.Jobs[i].State != "running" {
		return
	}
	s.state.Jobs[i].Progress = math.Max(0, math.Min(0.99, v))
	if now.Sub(*last) >= progressEventEvery {
		*last = now
		s.emit("shrink")
	}
}

func evictable(j Job, swaps map[string]swapIntent) bool {
	if _, pending := swaps[j.ID]; pending {
		return false
	}
	switch j.State {
	case "done":
		return j.Rescanned
	case "failed", "cancelled", "not-smaller", "changed":
		return true
	default:
		return false
	}
}

// topMountIsCIFS reads findmnt's FSTYPE lines for one mount point. Mounts can
// be stacked there, for example by systemd's namespace setup; the last line is
// the mount that paths under it resolve to.
func topMountIsCIFS(out string) bool {
	lines := strings.Fields(out)
	return len(lines) > 0 && lines[len(lines)-1] == "cifs"
}
