// Package inbox processes local videos with one native FFmpeg worker. The
// source share stays read only; the agent reaches its loopback API.
package inbox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"iglo/internal/worker"
)

var ErrInvalid = errors.New("invalid Inbox request")

const maxJobs = 100

type Settings struct {
	Paused  *bool  `json:"paused"`
	Profile string `json:"profile"`
}

type Input struct {
	Path    string `json:"path"`
	Profile string `json:"profile"`
}
type Job struct {
	ID string `json:"id"`
	Input
	Size       int64     `json:"sourceBytes"`
	Modified   time.Time `json:"sourceModified"`
	State      string    `json:"state"`
	Error      string    `json:"error,omitempty"`
	File       string    `json:"file,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}
type state struct {
	Paused  bool            `json:"paused"`
	Profile string          `json:"profile"`
	Jobs    []Job           `json:"jobs"`
	Seen    map[string]bool `json:"seen"`
}
type View struct {
	Backend      string `json:"backend"`
	StorageReady bool   `json:"storageReady"`
	Paused       bool   `json:"paused"`
	Admitted     bool   `json:"admitted"`
	Profile      string `json:"profile"`
	GPU          bool   `json:"gpu"`
	Jobs         []Job  `json:"jobs"`
	ScanError    string `json:"scanError,omitempty"`
	Sample       bool   `json:"sample,omitempty"`
}
type Service struct {
	Dir, Source, Output string
	Execute             func(context.Context, string, string, string) (string, error)
	MountOK             func() bool
	FreeBytes           func() (uint64, error)
	GPU                 bool
	mu                  sync.Mutex
	scan                sync.Mutex
	state               state
	scanError           string
	wake                chan struct{}
	subscribers         map[chan struct{}]struct{}
	scanWake            chan struct{}
	AdmissionDir        string
}

func validProfile(profile string) bool { return profile == "preserve" || profile == "compatible" }
func Validate(in Input) error {
	if !fs.ValidPath(in.Path) || in.Path == "." || len(in.Path) > 512 || strings.Contains(in.Path, "\\") || strings.ContainsFunc(in.Path, unicode.IsControl) || !validProfile(in.Profile) {
		return fmt.Errorf("%w: choose a video path inside Inbox and a supported profile", ErrInvalid)
	}
	for _, part := range strings.Split(in.Path, "/") {
		if strings.HasPrefix(part, ".") {
			return fmt.Errorf("%w: hidden paths are not processed", ErrInvalid)
		}
	}
	if !slices.Contains([]string{".mp4", ".m4v", ".mov", ".mkv", ".webm", ".avi", ".ts", ".mpeg", ".mpg"}, strings.ToLower(filepath.Ext(in.Path))) {
		return fmt.Errorf("%w: unsupported video extension", ErrInvalid)
	}
	return nil
}
func fingerprint(path string, info fs.FileInfo) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano())))
	return hex.EncodeToString(sum[:])
}
func New(dir, source, output string) (*Service, error) {
	for _, path := range []string{source, output} {
		if info, err := os.Lstat(path); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("Inbox roots must be real directories")
		}
	}
	var err error
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	output, err = filepath.EvalSymlinks(output)
	if err != nil {
		return nil, err
	}
	encoder := NewEncoder()
	s := &Service{AdmissionDir: filepath.Join(dir, "admission"), Dir: dir, Source: source, Output: output, Execute: encoder.Convert, GPU: encoder.GPU, wake: make(chan struct{}, 1), subscribers: map[chan struct{}]struct{}{}, scanWake: make(chan struct{}, 1)}
	s.MountOK = func() bool {
		for _, path := range []string{source, filepath.Dir(output)} {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			result, err := exec.CommandContext(ctx, "findmnt", "--mountpoint", path, "--noheadings", "--output", "FSTYPE").Output()
			cancel()
			if err != nil || strings.TrimSpace(string(result)) != "cifs" {
				return false
			}
		}
		return true
	}
	s.FreeBytes = func() (uint64, error) {
		var status syscall.Statfs_t
		err := syscall.Statfs(dir, &status)
		return status.Bavail * uint64(status.Bsize), err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s.state = state{Paused: true, Profile: "preserve", Jobs: []Job{}, Seen: map[string]bool{}}
	data, err := os.ReadFile(filepath.Join(dir, "queue.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(data) > 8<<20 {
		return nil, errors.New("Inbox queue is too large")
	}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &s.state); err != nil {
			return nil, err
		}
	}
	if !validProfile(s.state.Profile) || len(s.state.Jobs) > maxJobs || len(s.state.Seen) > 10000 {
		return nil, errors.New("invalid Inbox queue")
	}
	if s.state.Seen == nil {
		s.state.Seen = map[string]bool{}
	}
	changed := len(data) == 0
	for index := range s.state.Jobs {
		j := &s.state.Jobs[index]
		_, idErr := hex.DecodeString(j.ID)
		if Validate(j.Input) != nil || len(j.ID) != 32 || idErr != nil {
			return nil, errors.New("invalid saved Inbox job")
		}
		if j.State == "running" {
			j.State = "failed"
			j.Error = "Worker restarted during processing; inspect outputs before retrying"
			changed = true
		}
	}
	if err := os.RemoveAll(filepath.Join(dir, "staging")); err != nil {
		return nil, err
	}
	if changed {
		if err = s.save(); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *Service) notify() {
	for sub := range s.subscribers {
		select {
		case sub <- struct{}{}:
		default:
		}
	}
}
func (s *Service) save() error {
	if err := worker.Save(s.Dir, s.state); err != nil {
		return err
	}
	s.notify()
	return nil
}
func (s *Service) Status() View {
	ready := s.MountOK()
	s.mu.Lock()
	defer s.mu.Unlock()
	return View{Backend: "native", StorageReady: ready, Paused: s.state.Paused, Admitted: worker.Admitted(s.AdmissionDir), Profile: s.state.Profile, GPU: s.GPU, Jobs: slices.Clone(s.state.Jobs), ScanError: s.scanError}
}
func (s *Service) Settings(paused bool, profile string) error {
	if !validProfile(profile) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	oldPaused, oldProfile := s.state.Paused, s.state.Profile
	if oldPaused == paused && oldProfile == profile {
		return nil
	}
	s.state.Paused, s.state.Profile = paused, profile
	if err := s.save(); err != nil {
		s.state.Paused, s.state.Profile = oldPaused, oldProfile
		return err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}
func (s *Service) info(in Input) (fs.FileInfo, error) {
	if err := Validate(in); err != nil {
		return nil, err
	}
	current := s.Source
	for _, part := range strings.Split(in.Path, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, fmt.Errorf("%w: video does not exist", ErrInvalid)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: symbolic links are refused", ErrInvalid)
		}
	}
	info, err := os.Lstat(current)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxMediaBytes {
		return nil, fmt.Errorf("%w: use a regular video between 1 byte and 16 GiB", ErrInvalid)
	}
	if time.Since(info.ModTime()) < 30*time.Second {
		return nil, fmt.Errorf("%w: wait until the video has been unchanged for 30 seconds", ErrInvalid)
	}
	return info, nil
}
func (s *Service) Submit(in Input) (Job, error) {
	if !s.MountOK() {
		return Job{}, errors.New("Inbox or output share is unavailable")
	}
	info, err := s.info(in)
	if err != nil {
		return Job{}, err
	}
	return s.enqueue(in, info)
}
func (s *Service) enqueue(in Input, info fs.FileInfo) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := fingerprint(in.Path, info)
	if s.state.Seen[id] || slices.ContainsFunc(s.state.Jobs, func(j Job) bool {
		return j.Path == in.Path && j.Size == info.Size() && j.Modified.Equal(info.ModTime())
	}) {
		return Job{}, fmt.Errorf("%w: this file version is already queued or processed", ErrInvalid)
	}
	jobs := slices.Clone(s.state.Jobs)
	if len(jobs) >= maxJobs {
		index := slices.IndexFunc(jobs, func(j Job) bool { return j.State == "succeeded" || j.State == "failed" })
		if index < 0 {
			return Job{}, errors.New("Inbox queue is full")
		}
		jobs = slices.Delete(jobs, index, index+1)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Job{}, err
	}
	j := Job{ID: hex.EncodeToString(random[:]), Input: in, Size: info.Size(), Modified: info.ModTime(), State: "queued", CreatedAt: time.Now()}
	old := s.state.Jobs
	s.state.Jobs = append(jobs, j)
	s.state.Seen[id] = true
	if err := s.save(); err != nil {
		s.state.Jobs = old
		delete(s.state.Seen, id)
		return Job{}, err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return j, nil
}
func (s *Service) Scan() (int, error) {
	s.scan.Lock()
	defer s.scan.Unlock()
	if !s.MountOK() {
		return 0, errors.New("Inbox or output share is unavailable")
	}
	s.mu.Lock()
	profile := s.state.Profile
	s.mu.Unlock()
	type candidate struct {
		in   Input
		info fs.FileInfo
	}
	var paths []candidate
	present := map[string]bool{}
	entries := 0
	err := filepath.WalkDir(s.Source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		entries++
		if entries > 5000 {
			return errors.New("Inbox scan exceeds 5000 entries; organize a smaller inbox")
		}
		if path == s.Source {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(s.Source, path)
		if err != nil {
			return err
		}
		in := Input{Path: filepath.ToSlash(relative), Profile: profile}
		info, err := s.info(in)
		if err != nil {
			return nil
		}
		paths = append(paths, candidate{in, info})
		present[fingerprint(in.Path, info)] = true
		return nil
	})
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	changed := false
	oldSeen := maps.Clone(s.state.Seen)
	for _, j := range s.state.Jobs {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%d", j.Path, j.Size, j.Modified.UnixNano())))
		present[hex.EncodeToString(sum[:])] = true
	}
	for key := range s.state.Seen {
		if !present[key] {
			delete(s.state.Seen, key)
			changed = true
		}
	}
	if changed {
		err = s.save()
		if err != nil {
			s.state.Seen = oldSeen
		}
	}
	s.mu.Unlock()
	if err != nil {
		return 0, err
	}
	added := 0
	for _, candidate := range paths {
		if _, err := s.enqueue(candidate.in, candidate.info); err == nil {
			added++
		} else if !errors.Is(err, ErrInvalid) {
			return added, err
		}
	}
	return added, nil
}
func (s *Service) Run(ctx context.Context) {
	// SMB changes made on another client do not provide reliable inotify
	// signals. Scan at startup, on demand and every five minutes as fallback.
	scan := time.NewTicker(5 * time.Minute)
	defer scan.Stop()
	admission := time.NewTicker(10 * time.Second)
	defer admission.Stop()
	scanNow := func() {
		_, err := s.Scan()
		message := ""
		if err != nil {
			message = err.Error()
		}
		s.mu.Lock()
		if s.scanError != message {
			s.scanError = message
			s.notify()
		}
		s.mu.Unlock()
	}
	scanNow()
	for {
		for ctx.Err() == nil && s.step(ctx) {
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-admission.C:
		case <-s.scanWake:
			scanNow()
		case <-scan.C:
			scanNow()
		}
	}
}
func (s *Service) step(ctx context.Context) bool {
	s.mu.Lock()
	index := slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.State == "queued" })
	if index < 0 || s.state.Paused || !worker.Admitted(s.AdmissionDir) {
		s.mu.Unlock()
		return false
	}
	if !s.MountOK() {
		s.mu.Unlock()
		return false
	}
	s.state.Jobs[index].State = "running"
	job := s.state.Jobs[index]
	if err := s.save(); err != nil {
		s.state.Jobs[index].State = "queued"
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	process, cancel := context.WithTimeout(ctx, 6*time.Hour)
	file, err := s.process(process, job)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	index = slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.ID == job.ID })
	job.State = "succeeded"
	job.File = file
	job.FinishedAt = time.Now()
	if err != nil {
		job.State = "failed"
		job.Error = err.Error()
	}
	s.state.Jobs[index] = job
	if err = s.save(); err != nil {
		s.state.Paused = true
		return false
	}
	return true
}
func (s *Service) process(ctx context.Context, j Job) (string, error) {
	info, err := s.info(j.Input)
	if err != nil {
		return "", err
	}
	if info.Size() != j.Size || !info.ModTime().Equal(j.Modified) {
		return "", errors.New("source changed since it was queued")
	}
	free, err := s.FreeBytes()
	if err != nil || free < uint64(j.Size+maxMediaBytes+(4<<30)) {
		return "", errors.New("not enough SSD space for bounded staging and 4 GiB headroom")
	}
	stage := filepath.Join(s.Dir, "staging", j.ID)
	if err := os.MkdirAll(stage, 0700); err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	root, err := os.OpenRoot(s.Source)
	if err != nil {
		return "", err
	}
	defer root.Close()
	input, err := root.Open(filepath.FromSlash(j.Path))
	if err != nil {
		return "", err
	}
	defer input.Close()
	source := filepath.Join(stage, "source"+strings.ToLower(filepath.Ext(j.Path)))
	local, err := os.OpenFile(source, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	written, copyErr := worker.Copy(ctx, local, io.LimitReader(input, j.Size+1))
	closeErr := local.Close()
	after, statErr := input.Stat()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if statErr != nil || written != j.Size || after.Size() != j.Size || !after.ModTime().Equal(j.Modified) {
		return "", errors.New("source changed during staging")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	output, err := s.Execute(ctx, source, stage, j.Profile)
	if err != nil {
		return "", err
	}
	return worker.Publish(ctx, output, s.Output, "", j.ID+filepath.Ext(output), maxMediaBytes, s.MountOK)
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /jobs", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.Status()) })
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		var in Input
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&in) != nil {
			http.Error(w, "invalid video request", 400)
			return
		}
		j, err := s.Submit(in)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		write(w, 202, j)
	})
	mux.HandleFunc("POST /scan", func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.scanWake <- struct{}{}:
		default:
		}
		w.WriteHeader(202)
	})
	mux.HandleFunc("PUT /settings", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Paused  *bool  `json:"paused"`
			Profile string `json:"profile"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512)).Decode(&in) != nil || in.Paused == nil {
			http.Error(w, "give paused and profile", 400)
			return
		}
		if err := s.Settings(*in.Paused, in.Profile); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /jobs/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		s.mu.Lock()
		defer s.mu.Unlock()
		index := slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.ID == id && j.State == "failed" })
		if index < 0 {
			http.Error(w, "only failed jobs can be retried", 400)
			return
		}
		old := s.state.Jobs[index]
		s.state.Jobs[index].State = "queued"
		s.state.Jobs[index].Error = ""
		s.state.Jobs[index].FinishedAt = time.Time{}
		if err := s.save(); err != nil {
			s.state.Jobs[index] = old
			http.Error(w, "could not save retry", 500)
			return
		}
		select {
		case s.wake <- struct{}{}:
		default:
		}
		w.WriteHeader(202)
	})
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		changed := make(chan struct{}, 1)
		s.mu.Lock()
		s.subscribers[changed] = struct{}{}
		s.mu.Unlock()
		defer func() { s.mu.Lock(); delete(s.subscribers, changed); s.mu.Unlock() }()
		w.Header().Set("Content-Type", "text/event-stream")
		controller := http.NewResponseController(w)
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		fmt.Fprint(w, ": open\n\n")
		for controller.Flush() == nil {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fmt.Fprint(w, ": keep-alive\n\n")
			case <-changed:
				fmt.Fprint(w, "event: jobs\ndata: \n\n")
			}
		}
	})
	return mux
}
