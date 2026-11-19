package apps

import (
	"context"
	"errors"
	"time"

	"iglo/internal/shrink"
)

const (
	// autoShrinkEvery is the pace of the automatic pass while it is on. A new
	// file only becomes ready 30 minutes after its last change, so a faster
	// pass would find nothing more. A pass with nothing new costs about five
	// pct exec calls and one scan of the share.
	autoShrinkEvery = 30 * time.Minute
	// autoShrinkScanWait bounds the wait for the scan that a pass asks for.
	autoShrinkScanWait = 10 * time.Minute
	// autoShrinkBatch keeps one pass to a few hours of encoding, well under
	// the worker's 100-job limit.
	autoShrinkBatch = 20
	// autoShrinkMaxShare is the largest estimate, as a share of the file, that
	// the automatic pass accepts. 0.4 means it saves at least 60 percent.
	// Files that would save less stay for a manual choice.
	autoShrinkMaxShare = 0.4
)

type autoIdentity struct {
	bytes    int64
	modified time.Time
}

// autoShrinkCandidates picks ready anime files that the automatic pass has
// not queued before, that Sonarr did not refuse in their current form, and
// that the profile shrinks enough.
func autoShrinkCandidates(lib shrink.Library, profile string, refused map[string]autoIdentity) []string {
	var out []string
	var walk func(shrink.Folder)
	walk = func(f shrink.Folder) {
		for _, file := range f.Files {
			if file.Status != "ready" || file.AutoTried || file.Bytes <= 0 {
				continue
			}
			if r, ok := refused[file.Path]; ok && r.bytes == file.Bytes && r.modified.Equal(file.Modified) {
				continue
			}
			estimate := file.Estimates.High
			if profile == "standard" {
				estimate = file.Estimates.Standard
			}
			if estimate <= 0 || float64(estimate) > autoShrinkMaxShare*float64(file.Bytes) {
				continue
			}
			out = append(out, file.Path)
		}
		for _, sub := range f.Folders {
			walk(sub)
		}
	}
	for _, kind := range lib.Kinds {
		if kind.Path == "anime" {
			walk(kind)
		}
	}
	return out
}

func (s *Service) wakeAutoShrink() {
	select {
	case s.autoShrinkWake <- struct{}{}:
	default:
	}
}

// WatchAutoShrink queues new anime files while the Video tools setting is on.
// It reads the setting from the shared shrink snapshot, so it costs nothing
// more while the setting is off.
func (s *Service) WatchAutoShrink(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	refused := map[string]autoIdentity{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-s.autoShrinkWake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		c, cancel := context.WithTimeout(ctx, autoShrinkScanWait+5*time.Minute)
		if err := s.autoShrinkPass(c, refused); err != nil && ctx.Err() == nil {
			s.Logger.Warn("automatic shrink pass failed", "error", err)
		}
		cancel()
		timer.Reset(autoShrinkEvery)
	}
}

func (s *Service) autoShrinkPass(ctx context.Context, refused map[string]autoIdentity) error {
	a, id, err := s.shrinkWorker(ctx)
	if errors.Is(err, ErrNotInstalled) || errors.Is(err, ErrPlanned) {
		return nil
	}
	if err != nil {
		return err
	}
	view, err, _ := s.shrinkStatus.get(ctx, a, id, s.Logger)
	if err != nil {
		return err
	}
	profile := view.AutoAnime
	// The worker scans between jobs only, so a scan asked for during an encode
	// would wait for it. The next pass after the queue drains picks up new files.
	if profile == "" || shrinkActive(view) {
		return nil
	}
	lib, err := s.freshLibrary(ctx, a, id)
	if err != nil {
		return err
	}
	paths := autoShrinkCandidates(lib, profile, refused)
	if len(paths) == 0 {
		return nil
	}
	if len(paths) > autoShrinkBatch {
		paths = paths[:autoShrinkBatch]
	}
	out, err := s.SubmitShrink(ctx, shrink.Input{Paths: paths, Profile: profile, Confirm: "replace-originals", Auto: true})
	if err != nil {
		return err
	}
	files := map[string]shrink.File{}
	collectFiles(lib.Kinds, files)
	for _, r := range out.Refused {
		for _, p := range r.Paths {
			f := files[p]
			refused[p] = autoIdentity{bytes: f.Bytes, modified: f.Modified}
		}
	}
	s.Logger.Info("automatic shrink queued new anime", "queued", len(out.Jobs), "refused", len(out.Refused), "profile", profile)
	return nil
}

// freshLibrary asks for a scan and returns the library once that scan has
// finished. Library events wake it early; a one-minute check is the fallback.
func (s *Service) freshLibrary(ctx context.Context, a shrinkAgent, id int) (shrink.Library, error) {
	before, err := a.Library(ctx, id)
	if err != nil {
		return shrink.Library{}, err
	}
	if err := a.ScanLibrary(ctx, id); err != nil {
		return shrink.Library{}, err
	}
	deadline := time.NewTimer(autoShrinkScanWait)
	defer deadline.Stop()
	check := time.NewTicker(time.Minute)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return shrink.Library{}, ctx.Err()
		case <-deadline.C:
			return shrink.Library{}, errors.New("library scan did not finish in time")
		case <-s.autoLibrary:
		case <-check.C:
		}
		lib, err := a.Library(ctx, id)
		if err != nil {
			return shrink.Library{}, err
		}
		if !lib.Scanning && !lib.ScannedAt.Equal(before.ScannedAt) {
			if lib.Error != "" {
				return shrink.Library{}, errors.New(lib.Error)
			}
			return lib, nil
		}
	}
}

func collectFiles(folders []shrink.Folder, out map[string]shrink.File) {
	for _, f := range folders {
		for _, file := range f.Files {
			out[file.Path] = file
		}
		collectFiles(f.Folders, out)
	}
}
