// Package music provides one durable download worker. It has no network-facing
// listener: the host agent reaches its loopback API through pct exec.
package music

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"iglo/internal/worker"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxFile = 512 << 20

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type Input struct {
	URL   string `json:"url"`
	Video bool   `json:"video"`
}
type Job struct {
	ID string `json:"id"`
	Input
	State      string    `json:"state"`
	Error      string    `json:"error,omitempty"`
	File       string    `json:"file,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}
type View struct {
	StorageReady bool  `json:"storageReady"`
	Paused       bool  `json:"paused"`
	Admitted     bool  `json:"admitted"`
	Jobs         []Job `json:"jobs"`
	Sample       bool  `json:"sample,omitempty"`
}
type disk struct {
	Paused bool  `json:"paused"`
	Jobs   []Job `json:"jobs"`
}
type Service struct {
	Dir, Library string
	Execute      func(context.Context, Input, string) error
	MountOK      func() bool
	mu           sync.Mutex
	state        disk
	wake         chan struct{}
	// watchers are the open event streams. Each hears when the queue changes.
	watchMu  sync.Mutex
	watchers map[chan struct{}]struct{}
}

func Canonical(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return "", errors.New("use an HTTPS YouTube or YouTube Music video URL")
	}
	id := ""
	switch u.Hostname() {
	case "youtube.com", "www.youtube.com", "music.youtube.com":
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		}
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	}
	if !videoID.MatchString(id) {
		return "", errors.New("use one YouTube video URL; playlists and arbitrary sites are not accepted")
	}
	return "https://www.youtube.com/watch?v=" + id, nil
}
func New(dir, library string) (*Service, error) {
	s := &Service{Dir: dir, Library: library, Execute: download, wake: make(chan struct{}, 1)}
	s.MountOK = func() bool {
		out, err := exec.Command("findmnt", "--mountpoint", library, "--noheadings", "--output", "FSTYPE").Output()
		return err == nil && strings.TrimSpace(string(out)) == "cifs"
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "queue.json"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &s.state); err != nil {
			return nil, err
		}
	}
	if s.state.Jobs == nil {
		s.state.Jobs = []Job{}
	}
	// Staging is disposable; interrupted jobs require explicit resubmission.
	if err := os.RemoveAll(filepath.Join(dir, "staging")); err != nil {
		return nil, err
	}
	for n := range s.state.Jobs {
		if s.state.Jobs[n].State == "running" {
			s.state.Jobs[n].State = "failed"
			s.state.Jobs[n].Error = "Worker restarted during processing; submit again to retry"
		}
	}
	if err = s.save(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Service) save() error {
	if err := worker.Save(s.Dir, s.state); err != nil {
		return err
	}
	s.notify()
	return nil
}

// notify tells the open event streams that the queue changed. It never blocks.
func (s *Service) notify() {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	for watcher := range s.watchers {
		select {
		case watcher <- struct{}{}:
		default:
		}
	}
}

// watch returns a channel that hears when the queue changes, and a function that ends it.
func (s *Service) watch() (<-chan struct{}, func()) {
	watcher := make(chan struct{}, 1)
	s.watchMu.Lock()
	if s.watchers == nil {
		s.watchers = map[chan struct{}]struct{}{}
	}
	s.watchers[watcher] = struct{}{}
	s.watchMu.Unlock()
	return watcher, func() {
		s.watchMu.Lock()
		delete(s.watchers, watcher)
		s.watchMu.Unlock()
	}
}
func (s *Service) admitted() bool { return worker.Admitted(s.Dir) }

func (s *Service) Status() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return View{Paused: s.state.Paused, Admitted: s.admitted(), StorageReady: s.MountOK(), Jobs: slices.Clone(s.state.Jobs)}
}
func (s *Service) Pause(paused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.state.Paused
	s.state.Paused = paused
	if err := s.save(); err != nil {
		s.state.Paused = old
		return err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}
func (s *Service) Submit(in Input) (Job, error) {
	canonical, err := Canonical(in.URL)
	if err != nil {
		return Job{}, err
	}
	in.URL = canonical
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.state.Jobs {
		if j.URL == in.URL && j.Video == in.Video && j.State != "failed" {
			return Job{}, errors.New("this video is already queued or completed")
		}
	}
	if len(s.state.Jobs) >= 100 {
		n := slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.State == "succeeded" || j.State == "failed" })
		if n < 0 {
			return Job{}, errors.New("the queue is full")
		}
		s.state.Jobs = slices.Delete(s.state.Jobs, n, n+1)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Job{}, err
	}
	j := Job{ID: hex.EncodeToString(id[:]), Input: in, State: "queued", CreatedAt: time.Now()}
	old := slices.Clone(s.state.Jobs)
	s.state.Jobs = append(s.state.Jobs, j)
	if err = s.save(); err != nil {
		s.state.Jobs = old
		return Job{}, err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return j, nil
}
func (s *Service) Run(ctx context.Context) {
	// New jobs and pausing wake the loop. The ticker only notices a new
	// admission lease or a share that came back.
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
		for ctx.Err() == nil {
			if !s.step(ctx) {
				break
			}
		}
	}
}
func (s *Service) step(ctx context.Context) bool {
	s.mu.Lock()
	// Look for a job first. The admission and share checks read a file and
	// start findmnt, which an empty queue does not need.
	index := slices.IndexFunc(s.state.Jobs, func(j Job) bool { return j.State == "queued" })
	if index < 0 || s.state.Paused || !s.admitted() || !s.MountOK() {
		s.mu.Unlock()
		return false
	}
	s.state.Jobs[index].State = "running"
	j := s.state.Jobs[index]
	if err := s.save(); err != nil {
		s.state.Jobs[index].State = "queued"
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	file, err := s.process(ctx, j)
	s.mu.Lock()
	defer s.mu.Unlock()
	index = slices.IndexFunc(s.state.Jobs, func(job Job) bool { return job.ID == j.ID })
	if index < 0 {
		return false
	}
	j.State = "succeeded"
	j.File = file
	j.FinishedAt = time.Now()
	if err != nil {
		j.State = "failed"
		j.Error = err.Error()
	}
	s.state.Jobs[index] = j
	if err = s.save(); err != nil {
		s.state.Paused = true
		return false
	}
	return true
}
func (s *Service) process(ctx context.Context, j Job) (string, error) {
	stage := filepath.Join(s.Dir, "staging", j.ID)
	if err := os.MkdirAll(stage, 0700); err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	if err := s.Execute(ctx, j.Input, stage); err != nil {
		return "", err
	}
	ext, folder := ".m4a", "audio"
	if j.Video {
		ext, folder = ".mp4", "videos"
	} else {
		// Preserve the best source audio codec. Extraction/remuxing should not
		// introduce an extra lossy conversion just to enforce one extension.
		entries, err := os.ReadDir(stage)
		if err != nil {
			return "", err
		}
		found := false
		for _, entry := range entries {
			candidate := filepath.Ext(entry.Name())
			if strings.TrimSuffix(entry.Name(), candidate) == "clip" && slices.Contains([]string{".m4a", ".opus", ".mp3", ".flac", ".ogg", ".wav"}, candidate) {
				if found {
					return "", errors.New("download produced multiple audio outputs")
				}
				found = true
				ext = candidate
			}
		}
		if !found {
			return "", errors.New("download produced no supported audio output")
		}
	}
	source := filepath.Join(stage, "clip"+ext)
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxFile {
		return "", errors.New("download did not produce a valid bounded media file")
	}
	return worker.Publish(ctx, source, s.Library, folder, j.ID+ext, maxFile, s.MountOK)
}

// Keep diagnostics bounded even if an upstream program produces excessive output.
type tail struct{ data []byte }

func (t *tail) Write(p []byte) (int, error) {
	n := len(p)
	t.data = append(t.data, p...)
	if len(t.data) > 8192 {
		t.data = slices.Clone(t.data[len(t.data)-8192:])
	}
	return n, nil
}
func download(ctx context.Context, in Input, stage string) error {
	args := []string{"--ignore-config", "--no-plugin-dirs", "--no-playlist", "--no-cache-dir", "--no-progress", "--no-overwrites", "--socket-timeout", "20", "--retries", "3", "--concurrent-fragments", "1", "--max-filesize", "512M", "--match-filters", "duration <= 1200", "--paths", stage, "--output", "clip.%(ext)s", "--embed-metadata", "--embed-thumbnail", "--convert-thumbnails", "jpg", "--postprocessor-args", "ffmpeg:-threads 2"}
	if in.Video {
		args = append(args, "--format", "bestvideo[height<=1080]+bestaudio/best[height<=1080]", "--merge-output-format", "mp4", "--recode-video", "mp4")
	} else {
		args = append(args, "--format", "bestaudio", "--extract-audio", "--audio-format", "best")
	}
	args = append(args, "--", in.URL)
	cmd := exec.CommandContext(ctx, "/usr/local/bin/yt-dlp", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
	var log tail
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("download failed: %w: %s", err, strings.TrimSpace(string(log.data)))
	}
	return nil
}
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, status int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(value)
	}
	mux.HandleFunc("GET /jobs", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.Status()) })
	// /events says "jobs" when the queue changes, for the manager.
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		changed, stop := s.watch()
		defer stop()
		w.Header().Set("Content-Type", "text/event-stream")
		controller := http.NewResponseController(w)
		keepAlive := time.NewTicker(25 * time.Second)
		defer keepAlive.Stop()
		fmt.Fprint(w, ": open\n\n")
		for controller.Flush() == nil {
			select {
			case <-r.Context().Done():
				return
			case <-keepAlive.C:
				fmt.Fprint(w, ": keep-alive\n\n")
			case <-changed:
				fmt.Fprint(w, "event: jobs\ndata: \n\n")
			}
		}
	})
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		var in Input
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&in) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		j, err := s.Submit(in)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		write(w, 202, j)
	})
	mux.HandleFunc("PUT /pause", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Paused *bool `json:"paused"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&in) != nil || in.Paused == nil {
			http.Error(w, "give paused true or false", 400)
			return
		}
		if err := s.Pause(*in.Paused); err != nil {
			http.Error(w, "could not save pause", 500)
			return
		}
		w.WriteHeader(204)
	})
	return mux
}
