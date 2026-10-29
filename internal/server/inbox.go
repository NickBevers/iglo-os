package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"iglo/internal/inbox"
)

type inboxApps interface {
	InboxJobs(context.Context) (inbox.View, error)
	SubmitInbox(context.Context, inbox.Input) (inbox.Job, error)
	InboxCommand(context.Context, string, inbox.Settings) error
}

func (s *server) inboxWork(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Apps.(inboxApps)
	if !ok {
		writeError(w, 503, "update Iglo-OS to manage Inbox work")
		return
	}
	result := func(err error, code int) {
		if errors.Is(err, inbox.ErrInvalid) {
			writeError(w, 400, err.Error())
			return
		}
		s.writeAppsResult(w, err, code)
	}
	if r.Method == "GET" {
		v, err := a.InboxJobs(r.Context())
		if err != nil {
			result(err, 0)
			return
		}
		writeJSON(w, 200, v)
		return
	}
	switch r.URL.Path {
	case "/api/inbox":
		var in inbox.Input
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&in) != nil || inbox.Validate(in) != nil {
			writeError(w, 400, "give a video path inside Inbox and a supported profile")
			return
		}
		j, err := a.SubmitInbox(r.Context(), in)
		if err != nil {
			result(err, 0)
			return
		}
		writeJSON(w, 202, j)
	case "/api/inbox/settings":
		var in inbox.Settings
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512)).Decode(&in) != nil {
			writeError(w, 400, "invalid settings")
			return
		}
		result(a.InboxCommand(r.Context(), "settings", in), 204)
	case "/api/inbox/scan":
		result(a.InboxCommand(r.Context(), "scan", inbox.Settings{}), 202)
	default:
		result(a.InboxCommand(r.Context(), "retry/"+r.PathValue("id"), inbox.Settings{}), 202)
	}
}
