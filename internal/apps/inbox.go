package apps

import (
	"context"
	"encoding/hex"
	"reflect"
	"strings"
	"time"

	"iglo/internal/events"
	"iglo/internal/inbox"
	"iglo/internal/proxmox"
	"iglo/internal/shrink"
)

type inboxAgent interface {
	InboxJobs(context.Context, int) (inbox.View, error)
	SubmitInbox(context.Context, int, inbox.Input) (inbox.Job, error)
	InboxCommand(context.Context, int, string, any) error
}

func (s *Service) InboxJobs(ctx context.Context) (inbox.View, error) {
	_, g, err := s.runningApp(ctx, "inbox", "inbox")
	if err != nil {
		return inbox.View{}, err
	}
	a, ok := s.Agent.(inboxAgent)
	if !ok {
		return inbox.View{}, ErrPlanned
	}
	return a.InboxJobs(ctx, g.VMID)
}
func (s *Service) SubmitInbox(ctx context.Context, in inbox.Input) (inbox.Job, error) {
	if err := inbox.Validate(in); err != nil {
		return inbox.Job{}, err
	}
	_, g, err := s.runningApp(ctx, "inbox", "inbox")
	if err != nil {
		return inbox.Job{}, err
	}
	a, ok := s.Agent.(inboxAgent)
	if !ok {
		return inbox.Job{}, ErrPlanned
	}
	return a.SubmitInbox(ctx, g.VMID, in)
}
func (s *Service) InboxCommand(ctx context.Context, command string, in inbox.Settings) error {
	if command != "scan" && command != "settings" {
		id, ok := strings.CutPrefix(command, "retry/")
		decoded, err := hex.DecodeString(id)
		if !ok || err != nil || len(decoded) != 16 {
			return inbox.ErrInvalid
		}
	}
	if command == "settings" && (in.Paused == nil || inbox.Validate(inbox.Input{Path: "clip.mp4", Profile: in.Profile}) != nil) {
		return inbox.ErrInvalid
	}
	_, g, err := s.runningApp(ctx, "inbox", "inbox")
	if err != nil {
		return err
	}
	a, ok := s.Agent.(inboxAgent)
	if !ok {
		return ErrPlanned
	}
	return a.InboxCommand(ctx, g.VMID, command, in)
}
func (s *Service) WatchInbox(ctx context.Context) {
	a, ok := s.Agent.(interface {
		InboxEvents(context.Context, int, func(string)) error
	})
	if !ok {
		return
	}
	follow := newFollower(func(ctx context.Context, id int, _ func(string)) error {
		return a.InboxEvents(ctx, id, func(topic string) {
			switch topic {
			case "":
				s.shrinkStatus.forget(false)
				s.Changed(events.Inbox)
				s.Changed(events.Library)
				s.Changed(events.Shrink)
			case events.Inbox:
				s.Changed(events.Inbox)
				return
			case events.Library:
				s.Changed(events.Library)
				return
			case events.Shrink:
				// Publish after a changed snapshot is ready for all tabs to share.
			default:
				return
			}
			select {
			case s.shrinkChanged <- struct{}{}:
			default:
			}
		})
	})
	defer follow.stop()
	look := time.NewTicker(30 * time.Second)
	defer look.Stop()
	var poll *time.Timer
	var pollC <-chan time.Time
	defer func() {
		if poll != nil {
			poll.Stop()
		}
	}()
	schedule := func(delay time.Duration) {
		if poll != nil {
			poll.Stop()
			poll = nil
			pollC = nil
		}
		if delay > 0 {
			poll = time.NewTimer(delay)
			pollC = poll.C
		}
	}
	var guest proxmox.Resource
	retry := shrinkRetry{}
	var lastSnapshot *shrink.View
	update := func() {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, g, err := s.managedGuest(c, "inbox")
		cancel()
		if err != nil {
			g = proxmox.Resource{}
		}
		changed := g.VMID != guest.VMID || g.Status != guest.Status
		guest = g
		follow.keep(ctx, g.VMID, err == nil && g.Status == "running")
		if changed {
			s.wakeShrink()
		}
	}
	inspect := func(force, event bool) {
		if force {
			retry.next = time.Time{}
			retry.interval = 0
		}
		if delay := retry.remaining(); delay > 0 {
			schedule(delay)
			return
		}
		worker, ok := s.Agent.(shrinkAgent)
		if !ok || guest.Status != "running" {
			schedule(0)
			return
		}
		c, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		view, err, cached := s.shrinkStatus.get(c, worker, guest.VMID, s.Logger)
		if err != nil {
			schedule(max(time.Second, s.shrinkStatus.remaining()))
			return
		}
		if lastSnapshot == nil || !reflect.DeepEqual(*lastSnapshot, view) {
			snapshot := copyShrinkView(view)
			lastSnapshot = &snapshot
			s.Changed(events.Shrink)
		}
		pending := shrinkPending(view)
		// Progress only updates the shared snapshot. It never starts media work.
		if s.shrinkNeedsRescan(guest.VMID, view) {
			pending, err = s.rescanShrink(c, worker, guest.VMID, view)
			if err != nil {
				schedule(retry.failed(s.Logger, err))
				return
			}
			retry.recovered(s.Logger)
		}
		delay := time.Duration(0)
		if pending {
			delay = shrinkPoll
		}
		// A topic received during the cache lifetime gets one deferred read.
		// Its deadline stays fixed, so progress bursts cannot postpone completion.
		if event && cached {
			if remaining := s.shrinkStatus.remaining(); remaining > 0 && (delay == 0 || remaining < delay) {
				delay = remaining
			}
		}
		schedule(delay)
	}
	update()
	for {
		select {
		case <-ctx.Done():
			return
		case <-look.C:
			update()
		case <-s.shrinkWake:
			inspect(true, false)
		case <-s.shrinkChanged:
			inspect(false, true)
		case <-pollC:
			pollC = nil
			inspect(false, false)
		}
	}
}

func (s *Service) shrinkNeedsRescan(id int, view shrink.View) bool {
	for _, job := range view.Jobs {
		if job.State == "done" && !job.Rescanned {
			if s.shrinkVMID != id {
				return true
			}
			if _, ok := s.shrinkRescans[job.ID]; !ok {
				return true
			}
		}
	}
	return s.shrinkVMID == id && len(s.shrinkRescans) > 0 && !shrinkActive(view)
}
