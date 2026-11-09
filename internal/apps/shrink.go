package apps

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"iglo/internal/agent"
	"iglo/internal/events"
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
	view, err, _ := s.shrinkStatus.get(ctx, a, id, s.Logger)
	return view, err
}

// ValidateShrinkInput keeps the manager flow on exact Matroska file paths.
// The worker repeats this check and refuses directories, including ones whose
// name ends in .mkv. Files omitted from a dialog can never be expanded later.
func ValidateShrinkInput(in shrink.Input) error {
	if err := shrink.Validate(in); err != nil {
		return err
	}
	for _, p := range in.Paths {
		if len(strings.Split(p, "/")) < 3 || !strings.HasSuffix(strings.ToLower(p), ".mkv") {
			return shrink.ErrInvalid
		}
	}
	return nil
}

func (s *Service) SubmitShrink(ctx context.Context, in shrink.Input) (ShrinkSubmission, error) {
	out := ShrinkSubmission{Jobs: []shrink.Job{}, Refused: []arr.ShrinkCheckResult{}}
	if err := ValidateShrinkInput(in); err != nil {
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
		if command != "scan" {
			s.wakeShrink()
		}
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
	s.shrinkStatus.forget(true)
	select {
	case s.shrinkWake <- struct{}{}:
	default:
	}
}

func shrinkActive(view shrink.View) bool {
	for _, j := range view.Jobs {
		if j.State == "queued" || j.State == "running" {
			return true
		}
	}
	return false
}
func shrinkPending(view shrink.View) bool {
	if shrinkActive(view) {
		return true
	}
	for _, j := range view.Jobs {
		if j.State == "done" && !j.Rescanned {
			return true
		}
	}
	return false
}

// rescanShrink only handles final jobs. Successful media rescans wait in
// memory until the queue drains. Their durable jobs remain unacknowledged so
// a restart safely repeats the work. The bool records a successful Jellyfin
// refresh, so a failed acknowledgement does not refresh Jellyfin again.
func (s *Service) rescanShrink(ctx context.Context, a shrinkAgent, id int, view shrink.View) (bool, error) {
	if s.shrinkVMID != id {
		s.shrinkVMID = id
		s.shrinkRescans = map[string]bool{}
	}
	groups := map[string][]shrink.Job{}
	done := map[string]bool{}
	for _, j := range view.Jobs {
		if j.State != "done" || j.Rescanned {
			continue
		}
		done[j.ID] = true
		if _, ok := s.shrinkRescans[j.ID]; ok {
			continue
		}
		parts := strings.Split(j.Path, "/")
		if len(parts) < 3 {
			return true, shrink.ErrInvalid
		}
		title := strings.Join(parts[:2], "/")
		groups[title] = append(groups[title], j)
	}
	for id := range s.shrinkRescans {
		if !done[id] {
			delete(s.shrinkRescans, id)
		}
	}
	var roundErr error
	if len(groups) > 0 {
		_, media, err := s.runningApp(ctx, "media", "media")
		if err != nil && !errors.Is(err, ErrNotInstalled) {
			return true, err
		}
		for _, jobs := range groups {
			paths := make([]string, 0, len(jobs))
			for _, j := range jobs {
				paths = append(paths, j.Path)
			}
			if err == nil {
				if _, e := a.RescanLibrary(ctx, agent.MediaLibraryRequest{VMID: media.VMID, Paths: paths}); e != nil {
					roundErr = e
					continue
				}
			}
			for _, j := range jobs {
				s.shrinkRescans[j.ID] = false
			}
		}
	}
	if shrinkActive(view) {
		return true, roundErr
	}
	needsRefresh := false
	for _, refreshed := range s.shrinkRescans {
		if !refreshed {
			needsRefresh = true
		}
	}
	if needsRefresh {
		s.mu.Lock()
		refresh := s.libraryRefresh
		s.mu.Unlock()
		if refresh == nil {
			return true, errors.New("library refresh is not connected")
		}
		if err := refresh(ctx); err != nil {
			return true, err
		}
		for id := range s.shrinkRescans {
			s.shrinkRescans[id] = true
		}
		s.Changed(events.Jellyfin)
	}
	for jobID := range s.shrinkRescans {
		if err := a.ShrinkRescanned(ctx, id, jobID); err != nil {
			roundErr = err
			continue
		}
		delete(s.shrinkRescans, jobID)
		for i := range view.Jobs {
			if view.Jobs[i].ID == jobID {
				view.Jobs[i].Rescanned = true
			}
		}
		s.shrinkStatus.acknowledged(jobID)
		s.Changed(events.Shrink)
	}
	return shrinkPending(view), roundErr
}
