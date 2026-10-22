package apps

import (
	"context"
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
