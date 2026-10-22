package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"iglo/internal/workloads"
)

func (s *server) workloadsStatus(w http.ResponseWriter, r *http.Request) {
	if s.Workloads == nil {
		writeError(w, http.StatusServiceUnavailable, "background-work control is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, s.Workloads.Status())
}

func (s *server) workloadsSettings(w http.ResponseWriter, r *http.Request) {
	if s.Workloads == nil {
		writeError(w, http.StatusServiceUnavailable, "background-work control is unavailable")
		return
	}
	var input workloads.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.writeWorkloadsResult(w, s.Workloads.SaveSettings(input))
}

func (s *server) workloadsProtection(w http.ResponseWriter, r *http.Request) {
	if s.Workloads == nil {
		writeError(w, http.StatusServiceUnavailable, "background-work control is unavailable")
		return
	}
	var input struct {
		Minutes int `json:"minutes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.writeWorkloadsResult(w, s.Workloads.Protect(input.Minutes))
}

func (s *server) writeWorkloadsResult(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workloads.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		s.Logger.Error("save background-work controls", "error", err)
		writeError(w, http.StatusInternalServerError, "could not save background-work controls")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
