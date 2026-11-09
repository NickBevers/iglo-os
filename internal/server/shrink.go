package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"iglo/internal/apps"
	"iglo/internal/shrink"
)

type shrinkApps interface {
	Library(context.Context) (shrink.Library, error)
	ShrinkJobs(context.Context) (shrink.View, error)
	SubmitShrink(context.Context, shrink.Input) (apps.ShrinkSubmission, error)
	ShrinkCommand(context.Context, string, shrink.Settings) error
}

func shrinkBody(w http.ResponseWriter, r *http.Request, limit int64, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil && d.Decode(new(any)) == io.EOF
}
func (s *server) shrinkWork(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Apps.(shrinkApps)
	if !ok {
		writeError(w, 503, "update Iglo-OS to manage Video tools")
		return
	}
	result := func(err error, code int) {
		if errors.Is(err, shrink.ErrInvalid) {
			writeError(w, 400, err.Error())
			return
		}
		s.writeAppsResult(w, err, code)
	}
	switch r.URL.Path {
	case "/api/inbox/library":
		v, err := a.Library(r.Context())
		if err != nil {
			result(err, 0)
			return
		}
		writeJSON(w, 200, v)
	case "/api/inbox/shrink":
		if r.Method == "GET" {
			v, err := a.ShrinkJobs(r.Context())
			if err != nil {
				result(err, 0)
				return
			}
			writeJSON(w, 200, v)
			return
		}
		var in shrink.Input
		if !shrinkBody(w, r, 512<<10, &in) || apps.ValidateShrinkInput(in) != nil {
			writeError(w, 400, "give up to 100 library .mkv file paths, a supported profile and confirmation to replace originals")
			return
		}
		v, err := a.SubmitShrink(r.Context(), in)
		if err != nil {
			result(err, 0)
			return
		}
		writeJSON(w, 202, v)
	case "/api/inbox/library/scan":
		result(a.ShrinkCommand(r.Context(), "scan", shrink.Settings{}), 202)
	case "/api/inbox/shrink/settings":
		var in shrink.Settings
		if !shrinkBody(w, r, 512, &in) || in.Paused == nil {
			writeError(w, 400, "give paused")
			return
		}
		result(a.ShrinkCommand(r.Context(), "settings", in), 204)
	default:
		result(a.ShrinkCommand(r.Context(), "cancel/"+r.PathValue("id"), shrink.Settings{}), 204)
	}
}
