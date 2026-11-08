package apps

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"iglo/internal/agent"
	"iglo/internal/events"
	"iglo/internal/proxmox"
	"iglo/internal/shrink"
	"iglo/internal/stacks/arr"
)

type shrinkAgent interface {
	Library(context.Context, int) (shrink.Library, error)
	ScanLibrary(context.Context, int) error
	ShrinkJobs(context.Context, int) (shrink.View, error)
	SubmitShrink(context.Context, int, shrink.Input) ([]shrink.Job, error)
	ShrinkSettings(context.Context, int, bool) error
	CancelShrink(context.Context, int, string) error
	ShrinkRescanned(context.Context, int, string) error
	ShrinkCheck(context.Context, agent.MediaLibraryRequest) ([]arr.ShrinkCheckResult, error)
	RescanLibrary(context.Context, agent.MediaLibraryRequest) ([]arr.RescanResult, error)
}

type ShrinkSubmission struct {
	Jobs    []shrink.Job            `json:"jobs"`
	Refused []arr.ShrinkCheckResult `json:"refused"`
}

func (s *Service) shrinkWorker(ctx context.Context) (shrinkAgent, int, error) {
	_, g, err := s.runningApp(ctx, "inbox", "inbox")
	if err != nil {
		return nil, 0, err
	}
	a, ok := s.Agent.(shrinkAgent)
	if !ok {
		return nil, 0, ErrPlanned
	}
	return a, g.VMID, nil
}
func (s *Service) Library(ctx context.Context) (shrink.Library, error) {
	a, id, err := s.shrinkWorker(ctx)
	if err != nil {
		return shrink.Library{}, err
	}
	return a.Library(ctx, id)
}
func (s *Service) ShrinkJobs(ctx context.Context) (shrink.View, error) {
	a, id, err := s.shrinkWorker(ctx)
	if err != nil {
		return shrink.View{}, err
	}
	return a.ShrinkJobs(ctx, id)
}
func (s *Service) SubmitShrink(ctx context.Context, in shrink.Input) (ShrinkSubmission, error) {
	out := ShrinkSubmission{Jobs: []shrink.Job{}, Refused: []arr.ShrinkCheckResult{}}
	if err := shrink.Validate(in); err != nil {
		return out, err
	}
	a, id, err := s.shrinkWorker(ctx)
	if err != nil {
		return out, err
	}
	// Deduplicate before the guard, whose grouped results echo input paths.
	unique := make([]string, 0, len(in.Paths))
	seen := map[string]bool{}
	for _, p := range in.Paths {
		if !seen[p] {
			unique = append(unique, p)
			seen[p] = true
		}
	}
	in.Paths = unique
	_, media, err := s.runningApp(ctx, "media", "media")
	paths := in.Paths
	if err == nil {
		checks, e := a.ShrinkCheck(ctx, agent.MediaLibraryRequest{VMID: media.VMID, Paths: paths})
		if e != nil {
			return out, e
		}
		// Require an explicit decision for every requested path. Never infer safety
		// from a missing result, a parent prefix or an unknown response state.
		decisions := map[string]string{}
		for _, c := range checks {
			if c.State != "ok" && c.State != "ok-unmanaged" && c.State != "refused" {
				return out, errors.New("invalid shrink check response")
			}
			for _, p := range c.Paths {
				if _, exists := decisions[p]; exists {
					return out, errors.New("duplicate shrink check result")
				}
				decisions[p] = c.State
			}
			if c.State == "refused" {
				out.Refused = append(out.Refused, c)
			}
		}
		paths = nil
		for _, p := range in.Paths {
			state, ok := decisions[p]
			if !ok {
				return out, errors.New("incomplete shrink check response")
			}
			if state != "refused" {
				paths = append(paths, p)
			}
		}
	} else if !errors.Is(err, ErrNotInstalled) {
		return out, err
	}
	err = nil
	if len(paths) > 0 {
		in.Paths = paths
		out.Jobs, err = a.SubmitShrink(ctx, id, in)
		if err == nil {
			s.wakeShrink()
			s.Changed(events.Shrink)
		}
	}
	return out, err
}

func (s *Service) ShrinkCommand(ctx context.Context, command string, settings shrink.Settings) error {
	if command == "settings" {
		if settings.Paused == nil {
			return shrink.ErrInvalid
		}
	} else if command != "scan" {
		id, ok := strings.CutPrefix(command, "cancel/")
		b, e := hex.DecodeString(id)
		if !ok || e != nil || len(b) != 16 {
			return shrink.ErrInvalid
		}
	}
	a, id, err := s.shrinkWorker(ctx)
	if err != nil {
		return err
	}
	switch command {
	case "scan":
		err = a.ScanLibrary(ctx, id)
	case "settings":
		err = a.ShrinkSettings(ctx, id, *settings.Paused)
	default:
		err = a.CancelShrink(ctx, id, strings.TrimPrefix(command, "cancel/"))
	}
	if err == nil {
		s.wakeShrink()
		if command == "scan" {
			s.Changed(events.Library)
		} else {
			s.Changed(events.Shrink)
		}
	}
	return err
}

// SetLibraryRefresh connects the existing manager Jellyfin service. Server
// construction calls it without changing the command's startup wiring.
func (s *Service) SetLibraryRefresh(refresh func(context.Context) error) {
	s.mu.Lock()
	s.libraryRefresh = refresh
	s.mu.Unlock()
	s.wakeShrink()
}
func (s *Service) wakeShrink() {
	select {
	case s.shrinkWake <- struct{}{}:
	default:
	}
}

const shrinkPoll = time.Minute

func shrinkPending(view shrink.View) bool {
	for _, j := range view.Jobs {
		if j.State == "queued" || j.State == "running" || (j.State == "done" && !j.Rescanned) {
			return true
		}
	}
	return false
}

// rescanShrink runs only in WatchInbox, so rounds never overlap. Failed titles
// remain unacknowledged. A failed Jellyfin refresh also retries next round.
func (s *Service) rescanShrink(ctx context.Context, a shrinkAgent, id int) (bool, error) {
	view, err := a.ShrinkJobs(ctx, id)
	if err != nil {
		return true, err
	}
	groups := map[string][]shrink.Job{}
	for _, j := range view.Jobs {
		if j.State == "done" && !j.Rescanned {
			parts := strings.Split(j.Path, "/")
			if len(parts) < 2 {
				return true, shrink.ErrInvalid
			}
			title := strings.Join(parts[:2], "/")
			groups[title] = append(groups[title], j)
		}
	}
	if len(groups) == 0 {
		return shrinkPending(view), nil
	}
	_, media, mediaErr := s.runningApp(ctx, "media", "media")
	if mediaErr != nil && !errors.Is(mediaErr, ErrNotInstalled) {
		return true, mediaErr
	}
	completed := []shrink.Job{}
	var roundErr error
	for _, jobs := range groups {
		paths := make([]string, 0, len(jobs))
		for _, j := range jobs {
			paths = append(paths, j.Path)
		}
		if mediaErr == nil {
			if _, err := a.RescanLibrary(ctx, agent.MediaLibraryRequest{VMID: media.VMID, Paths: paths}); err != nil {
				roundErr = err
				continue
			}
		}
		completed = append(completed, jobs...)
	}
	if len(completed) > 0 {
		s.mu.Lock()
		refresh := s.libraryRefresh
		s.mu.Unlock()
		if refresh == nil {
			return true, errors.New("library refresh is not connected")
		}
		if err := refresh(ctx); err != nil {
			return true, err
		}
		for _, j := range completed {
			if err := a.ShrinkRescanned(ctx, id, j.ID); err != nil {
				roundErr = err
				continue
			}
			for i := range view.Jobs {
				if view.Jobs[i].ID == j.ID {
					view.Jobs[i].Rescanned = true
				}
			}
		}
		s.Changed(events.Shrink)
		s.Changed(events.Jellyfin)
	}
	return shrinkPending(view), roundErr
}

func (s *Service) shrinkRound(ctx context.Context, g proxmox.Resource) bool {
	a, ok := s.Agent.(shrinkAgent)
	if !ok || g.Status != "running" {
		return false
	}
	round, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	pending, err := s.rescanShrink(round, a, g.VMID)
	if err != nil {
		s.Logger.Warn("rescan shrunk library", "error", err)
	}
	return pending
}
