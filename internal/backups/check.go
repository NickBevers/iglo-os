package backups

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
)

const (
	// checkEvery is how often a restore check runs, on the next container each time.
	checkEvery = 30 * 24 * time.Hour
	// checkHour is when, after the nightly backups at 03:00 and the weekly
	// updates at 04:00.
	checkHour = 5
)

// restoreOnHost has the host agent restore, and waits until it is done.
func (s *Service) restoreOnHost(ctx context.Context, request agent.RestoreRequest) error {
	ctx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()

	// The agent's start time names this restore, so an older one can't answer.
	started, err := s.Agent.Restore(ctx, request)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(s.RestorePoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("the restore took too long: %w", ctx.Err())
		case <-ticker.C:
		}
		status, err := s.Agent.RestoreStatus(ctx)
		if err != nil {
			s.Logger.Warn("could not read the restore status", "error", err)
			continue
		}
		if status.Running || status.Request != request || !status.StartedAt.Equal(started.StartedAt) {
			continue
		}
		if !status.Succeeded {
			return fmt.Errorf("%s", status.Message)
		}
		return nil
	}
}

// Check restores a container backup to a copy without network or mounts,
// starts it once and removes it again. It proves that the backup restores,
// without touching the container or its data.
func (s *Service) Check(ctx context.Context, volid string) error {
	backup, _, err := s.findBackup(ctx, volid)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(backup.Format, "tar") {
		return fmt.Errorf("%w: only container backups can be checked", ErrRefused)
	}
	guest, err := s.findGuest(ctx, backup.VMID)
	if err != nil {
		return err
	}
	storage, err := s.Proxmox.GuestStorage(ctx, guest.Node, proxmox.LXC, guest.VMID)
	if err != nil {
		return err
	}

	return s.start(ctx, fmt.Sprintf("Checking a backup of %s", guest.Name), func(ctx context.Context) Run {
		run := s.newRun(RunCheck, guest)
		err := s.restoreOnHost(ctx, agent.RestoreRequest{VMID: guest.VMID, Volid: volid, Storage: storage, Check: true})

		s.mu.Lock()
		s.state.LastCheck, s.state.LastCheckVMID = s.Now(), guest.VMID
		s.mu.Unlock()

		return s.finish(run, err, "the backup of "+formatBackupTime(backup.CTime)+" restores and starts")
	})
}

// monthlyCheck checks the newest backup of the next container, once a month
// at night, unless something else runs or a game is on.
func (s *Service) monthlyCheck(ctx context.Context) {
	now := s.Now()
	s.mu.Lock()
	due := now.Hour() == checkHour && now.Sub(s.state.LastCheck) >= checkEvery && s.busy == ""
	after := s.state.LastCheckVMID
	s.mu.Unlock()
	if !due || (s.Hold != nil && s.Hold()) {
		return
	}

	view, err := s.Status(ctx)
	if err != nil {
		s.Logger.Warn("could not choose a backup to check", "error", err)
		return
	}
	volid := nextCheck(view.Guests, after)
	if volid == "" {
		return
	}
	if err := s.Check(ctx, volid); err != nil {
		s.Logger.Warn("could not start the restore check", "volid", volid, "error", err)
	}
}

// nextCheck is the newest backup of the first container after the one that
// was checked last, round the list.
func nextCheck(guests []GuestView, after int) string {
	var candidates []GuestView
	for _, guest := range guests {
		if guest.Type == "lxc" && len(guest.Backups) > 0 {
			candidates = append(candidates, guest)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	slices.SortFunc(candidates, func(a, b GuestView) int { return a.VMID - b.VMID })
	next := candidates[0]
	for _, guest := range candidates {
		if guest.VMID > after {
			next = guest
			break
		}
	}
	newest := slices.MaxFunc(next.Backups, func(a, b BackupView) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return newest.VolID
}

// CheckView is the last restore check and when the next one is due.
type CheckView struct {
	Last *Run      `json:"last,omitempty"`
	Next time.Time `json:"next"`
}

// checkView needs s.mu.
func (s *Service) checkView() CheckView {
	view := CheckView{}
	for _, run := range s.state.History {
		if run.Kind == RunCheck {
			view.Last = &run
			break
		}
	}
	next := s.state.LastCheck.Add(checkEvery)
	if next.Before(s.Now()) {
		next = s.Now()
	}
	view.Next = time.Date(next.Year(), next.Month(), next.Day(), checkHour, 0, 0, 0, next.Location())
	if view.Next.Before(s.Now()) {
		view.Next = view.Next.AddDate(0, 0, 1)
	}
	return view
}
