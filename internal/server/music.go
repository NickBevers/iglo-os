package server

import (
	"context"
	"encoding/json"
	"iglo/internal/music"
	"net/http"
)

type musicApps interface {
	MusicJobs(context.Context) (music.View, error)
	SubmitMusic(context.Context, music.Input) (music.Job, error)
	PauseMusic(context.Context, bool) error
}

func (s *server) musicJobs(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Apps.(musicApps)
	if !ok {
		writeError(w, 503, "update Iglo-OS to manage music work")
		return
	}
	if r.Method == http.MethodGet {
		v, err := a.MusicJobs(r.Context())
		if err != nil {
			s.writeAppsResult(w, err, 0)
			return
		}
		writeJSON(w, 200, v)
		return
	}
	if r.Method == http.MethodPost {
		var in music.Input
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&in) != nil {
			writeError(w, 400, "invalid request")
			return
		}
		if _, err := music.Canonical(in.URL); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		j, err := a.SubmitMusic(r.Context(), in)
		if err != nil {
			s.writeAppsResult(w, err, 0)
			return
		}
		writeJSON(w, 202, j)
		return
	}
	var in struct {
		Paused *bool `json:"paused"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&in) != nil || in.Paused == nil {
		writeError(w, 400, "give paused true or false")
		return
	}
	s.writeAppsResult(w, a.PauseMusic(r.Context(), *in.Paused), 204)
}
