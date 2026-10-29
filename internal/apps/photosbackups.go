package apps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"iglo/internal/agent"
	"iglo/internal/events"
)

// photosBackupAgent keeps the settings and the state of the Photos backups.
type photosBackupAgent interface {
	PhotosBackups(ctx context.Context) (agent.PhotosBackupView, error)
	SavePhotosBackups(ctx context.Context, settings agent.PhotosBackupSettings) error
}

const (
	// photosBackupTTL is how long the Apps view keeps the state of the Photos
	// backups. It only changes when a backup or a check ends, or the settings
	// change, and those forget it.
	photosBackupTTL = 10 * time.Minute
	// photosScheduleTTL is how long the scheduler keeps the settings. Saving
	// them from the Apps page forgets them at once.
	photosScheduleTTL = time.Hour
	// photosBackupDelay is how long the nightly backup may wait for backups or
	// updates that run, before it skips the night.
	photosBackupDelay = 6 * time.Hour
)

func (s *Service) photosBackupView(ctx context.Context, ttl time.Duration) (agent.PhotosBackupView, error) {
	a, ok := s.Agent.(photosBackupAgent)
	if !ok {
		return agent.PhotosBackupView{}, agent.ErrAppNotAvailable
	}
	return s.photoBackups.get("photos", ttl, func() (agent.PhotosBackupView, error) { return a.PhotosBackups(ctx) })
}

// SavePhotosBackups saves the settings of the Photos backups. The host checks
// that a folder off the NAS is on a disk of its own.
func (s *Service) SavePhotosBackups(ctx context.Context, settings agent.PhotosBackupSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if _, _, err := s.managedGuest(ctx, agent.PhotosApp); err != nil {
		return err
	}
	a, ok := s.Agent.(photosBackupAgent)
	if !ok {
		return agent.ErrAppNotAvailable
	}
	if err := a.SavePhotosBackups(ctx, settings); err != nil {
		return err
	}
	s.photoBackups.forget()
	s.Logger.Info("photos backup settings saved", "time", settings.Time, "offsite", settings.OffsitePath != "")
	s.Changed(events.Apps)
	return nil
}

// photosBackedUp is the line of backup.sh after the backup of an update.
const photosBackedUp = "Photos is backed up in snapshot"

// PhotosBackupRunning names the Photos backup or check that runs now, or is
// "". Heavy background work waits for it, like for the Proxmox backups. It
// counts the backup at the start of a Photos update too. The host agent says
// what runs, so it also holds after the manager restarted in the middle of a
// backup. Its answer is kept for 10 seconds: Background work asks up to 4
// times a minute, and the answer is a file read on the host, without pct.
func (s *Service) PhotosBackupRunning(ctx context.Context) string {
	s.mu.Lock()
	active, action := s.active, s.activeAction
	s.mu.Unlock()
	if active == agent.PhotosApp && action != agent.ActionUpdate {
		if reason := photosHoldReason(action, ""); reason != "" {
			return reason
		}
	}
	status, err := s.holdStatus.get("photos", 10*time.Second, func() (agent.AppInstallStatus, error) {
		return s.Agent.AppInstallStatus(ctx)
	})
	if err != nil || status.State != agent.UpgradeRunning || status.App != agent.PhotosApp {
		return ""
	}
	return photosHoldReason(status.Action, status.Log)
}

// photosHoldReason is why heavy work waits for a Photos operation with log.
func photosHoldReason(action agent.AppAction, log string) string {
	switch action {
	case agent.ActionBackup:
		return "The Photos backup is running"
	case agent.ActionVerify:
		return "The Photos backups are being checked"
	case agent.ActionUpdate:
		if !strings.Contains(log, photosBackedUp) {
			return "The Photos backup before its update is running"
		}
	}
	return ""
}

// RunPhotosBackups starts the nightly Photos backup at the time of its
// settings. It doesn't start while hold names something, like running
// Proxmox backups or updates, and tries again each minute for 6 hours.
// Outside that time, a tick costs a clock reading, and the settings come
// from the host agent once an hour.
func (s *Service) RunPhotosBackups(ctx context.Context, hold func(context.Context) string) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.photosBackupTick(ctx, now, hold)
		}
	}
}

// lastPhotosSlot is the last time of day at or before now, in the time zone of now.
func lastPhotosSlot(at string, now time.Time) time.Time {
	parsed, err := time.Parse("15:04", at)
	if err != nil {
		return time.Time{}
	}
	slot := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	if slot.After(now) {
		slot = time.Date(now.Year(), now.Month(), now.Day()-1, parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	}
	return slot
}

func (s *Service) photosBackupTick(ctx context.Context, now time.Time, hold func(context.Context) string) {
	if _, ok := s.Agent.(photosBackupAgent); !ok {
		return
	}
	view, err := s.photosBackupView(ctx, photosScheduleTTL)
	if err != nil {
		return
	}
	slot := lastPhotosSlot(view.Settings.Time, now)
	s.mu.Lock()
	handled, waitedFor := s.photosSlot, s.photosHold
	s.mu.Unlock()
	if slot.IsZero() || !slot.After(handled) {
		return
	}
	done := func(message string) {
		s.mu.Lock()
		s.photosSlot, s.photosHold = slot, ""
		s.mu.Unlock()
		if message != "" {
			s.Logger.Warn("nightly photos backup", "message", message)
			s.Notify(ctx, message)
		}
	}
	if now.Sub(slot) > photosBackupDelay {
		if waitedFor != "" {
			done(fmt.Sprintf("⚠️ The nightly Photos backup did not start in %d hours. It waited, because: %s.", int(photosBackupDelay.Hours()), strings.TrimSuffix(waitedFor, ".")))
			return
		}
		// The manager was off at the time: the next night runs again.
		done("")
		return
	}

	// A backup that started after the slot counts, like one by hand, or one
	// from before the manager restarted.
	s.photoBackups.forget()
	if fresh, err := s.photosBackupView(ctx, photosScheduleTTL); err == nil && !fresh.Status.Started.Before(slot) {
		done("")
		return
	}
	_, guest, err := s.managedGuest(ctx, agent.PhotosApp)
	if err != nil {
		// Without Photos there is nothing to back up.
		done("")
		return
	}
	if guest.Status != "running" {
		done(fmt.Sprintf("⚠️ The nightly Photos backup did not run: container %d is not running.", guest.VMID))
		return
	}
	if reason := hold(ctx); reason != "" {
		if waitedFor == "" {
			s.Logger.Info("nightly photos backup waits", "reason", reason)
		}
		s.mu.Lock()
		s.photosHold = reason
		s.mu.Unlock()
		return
	}
	err = s.RecoverPhotos(ctx, ctx, agent.ActionBackup)
	switch {
	case errors.Is(err, ErrBusy):
		// An install or an update runs: the next minute tries again.
		s.mu.Lock()
		s.photosHold = "An app is being installed or updated"
		s.mu.Unlock()
	case err != nil:
		done("❌ The nightly Photos backup could not start: " + err.Error())
	default:
		s.Logger.Info("nightly photos backup started", "slot", slot)
		done("")
	}
}

// photosRecoveryMessage is the notification after a Photos backup or check.
func (s *Service) photosRecoveryMessage(ctx context.Context, action agent.AppAction, status agent.AppInstallStatus) string {
	s.photoBackups.forget()
	view, err := s.photosBackupView(ctx, photosBackupTTL)
	state := view.Status
	if action == agent.ActionVerify {
		if status.State != agent.UpgradeSucceeded {
			detail := status.Message
			if err == nil && state.VerifyMessage != "" {
				detail = state.VerifyMessage
			}
			return "❌ Verify recovery of Photos failed: " + detail + "."
		}
		if err == nil && state.VerifySnapshot != "" {
			return fmt.Sprintf("✅ The Photos backups are checked. The database of snapshot %s restores.", state.VerifySnapshot)
		}
		return "✅ The Photos backups are checked."
	}

	if status.State != agent.UpgradeSucceeded {
		detail := status.Message
		if err == nil && state.Message != "" {
			detail = state.Message
		}
		return "❌ The Photos backup failed: " + detail + "."
	}
	if err != nil {
		return "✅ Photos is backed up to the NAS."
	}
	message := fmt.Sprintf("✅ Photos is backed up to the NAS in snapshot %s.", state.Snapshot)
	switch state.Offsite.Result {
	case "ok":
		message += " The copy off the NAS is up to date too."
	case "missing":
		message += " ⚠️ No copy off the NAS: " + state.Offsite.Message + "."
	case "failed":
		message += " ⚠️ The copy off the NAS failed."
	}
	return message
}
