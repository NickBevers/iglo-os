package apps

import (
	"context"
	"encoding/hex"
	"iglo/internal/events"
	"iglo/internal/inbox"
	"strings"
	"time"
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
	follow := newFollower(a.InboxEvents)
	defer follow.stop()
	for {
		look, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, g, err := s.managedGuest(look, "inbox")
		cancel()
		follow.keep(ctx, g.VMID, err == nil && g.Status == "running")
		timer := time.NewTimer(30 * time.Second)
	wait:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-follow.events:
				s.Changed(events.Inbox)
			case <-timer.C:
				break wait
			}
		}
	}
}
