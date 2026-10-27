package apps

import (
	"context"
	"time"

	"iglo/internal/agent"
	"iglo/internal/events"
	"iglo/internal/music"
)

type musicAgent interface {
	MusicJobs(context.Context, int) (music.View, error)
	SubmitMusic(context.Context, int, music.Input) (music.Job, error)
	PauseMusic(context.Context, int, bool) error
}

func (s *Service) MusicJobs(ctx context.Context) (music.View, error) {
	_, g, err := s.runningApp(ctx, "music", "music")
	if err != nil {
		return music.View{}, err
	}
	a, ok := s.Agent.(musicAgent)
	if !ok {
		return music.View{}, ErrPlanned
	}
	return a.MusicJobs(ctx, g.VMID)
}
func (s *Service) SubmitMusic(ctx context.Context, in music.Input) (music.Job, error) {
	_, g, err := s.runningApp(ctx, "music", "music")
	if err != nil {
		return music.Job{}, err
	}
	if err = s.checkIdle(ctx); err != nil {
		return music.Job{}, err
	}
	a, ok := s.Agent.(musicAgent)
	if !ok {
		return music.Job{}, ErrPlanned
	}
	return a.SubmitMusic(ctx, g.VMID, in)
}
func (s *Service) PauseMusic(ctx context.Context, paused bool) error {
	_, g, err := s.runningApp(ctx, "music", "music")
	if err != nil {
		return err
	}
	a, ok := s.Agent.(musicAgent)
	if !ok {
		return ErrPlanned
	}
	return a.PauseMusic(ctx, g.VMID, paused)
}

// musicEventsAgent follows the job events of the Music worker.
type musicEventsAgent interface {
	MusicEvents(ctx context.Context, vmid int, onEvent func(topic string)) error
}

// musicLookAgain is how often WatchMusic checks whether the Music container runs.
const musicLookAgain = 30 * time.Second

// WatchMusic follows the job events of the Music worker while its container
// runs, and tells the open pages when the queue changed.
func (s *Service) WatchMusic(ctx context.Context) {
	a, ok := s.Agent.(musicEventsAgent)
	if !ok {
		return
	}
	follow := newFollower(a.MusicEvents)
	defer follow.stop()
	for {
		lookCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, guest, err := s.managedGuest(lookCtx, agent.MusicApp)
		cancel()
		follow.keep(ctx, guest.VMID, err == nil && guest.Status == "running")
		timeout := time.After(musicLookAgain)
	wait:
		for {
			select {
			case <-ctx.Done():
				return
			case <-follow.events:
				s.Changed(events.Music)
			case <-timeout:
				break wait
			}
		}
	}
}
