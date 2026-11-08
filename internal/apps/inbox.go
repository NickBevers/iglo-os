package apps

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"iglo/internal/events"
	"iglo/internal/inbox"
	"iglo/internal/proxmox"
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
	// Publish names immediately, even while a rescan round waits on an app.
	// The buffered wake coalesces shrink events without dropping another topic.
	follow := newFollower(func(ctx context.Context, id int, _ func(string)) error {
		return a.InboxEvents(ctx, id, func(topic string) {
			switch topic {
			case "":
				s.Changed(events.Inbox)
				s.Changed(events.Library)
				s.Changed(events.Shrink)
				s.wakeShrink()
			case events.Inbox:
				s.Changed(events.Inbox)
			case events.Library:
				s.Changed(events.Library)
			case events.Shrink:
				s.Changed(events.Shrink)
				s.wakeShrink()
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
	var guest proxmox.Resource
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
	round := func() {
		if poll != nil {
			poll.Stop()
			poll = nil
			pollC = nil
		}
		if s.shrinkRound(ctx, guest) {
			poll = time.NewTimer(shrinkPoll)
			pollC = poll.C
		}
	}
	update()
	for {
		select {
		case <-ctx.Done():
			return
		case <-look.C:
			update()
		case <-s.shrinkWake:
			round()
		case <-pollC:
			pollC = nil
			round()
		}
	}
}
