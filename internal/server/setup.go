package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"iglo/internal/setup"
)

type Setup interface {
	View(ctx context.Context) setup.View
	Start(ctx context.Context, plan setup.Plan) error
	Retry() error
	Skip() error
	Cancel()
}

// setupRoute serves the guided setup: its state, a new run, and retry, skip
// or cancel after a failed step.
func (s *server) setupRoute(w http.ResponseWriter, r *http.Request) {
	if s.Setup == nil {
		writeError(w, http.StatusServiceUnavailable, "update Iglo-OS for the guided setup")
		return
	}
	var err error
	switch r.Method + " " + r.PathValue("action") {
	case "GET ":
		writeJSON(w, http.StatusOK, s.Setup.View(r.Context()))
		return
	case "POST ":
		var plan setup.Plan
		// A plan with all shares and accounts is a few KB.
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&plan) != nil {
			writeError(w, http.StatusBadRequest, "invalid setup")
			return
		}
		err = s.Setup.Start(r.Context(), plan)
	case "POST retry":
		err = s.Setup.Retry()
	case "POST skip":
		err = s.Setup.Skip()
	case "DELETE ":
		s.Setup.Cancel()
	default:
		writeError(w, http.StatusNotFound, "unknown setup action")
		return
	}
	switch {
	case errors.Is(err, setup.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusConflict, err.Error())
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}
