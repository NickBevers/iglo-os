package server

import (
	"context"
	"encoding/json"
	"net/http"

	"iglo/internal/apps"
)

type gamingApps interface {
	Gaming(context.Context) (apps.GamingView, error)
	SetGamingPower(context.Context, bool) error
}

// playMinutes is how long gaming protection lasts after Play, at most. Stop
// ends it sooner; the limit covers a session that you forget to stop.
const playMinutes = 480

func (s *server) gamingStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Apps.(gamingApps)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "update Iglo-OS to manage gaming")
		return
	}
	view, err := a.Gaming(r.Context())
	if err != nil {
		s.writeAppsResult(w, err, 0)
		return
	}
	if s.Workloads != nil {
		status := s.Workloads.Status()
		view.Protected, view.ProtectedUntil = status.GamingActive, status.GamingUntil
	}
	writeJSON(w, http.StatusOK, view)
}

// gamingPlay starts the gaming container with gaming protection, or shuts it
// down and ends the protection.
func (s *server) gamingPlay(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Apps.(gamingApps)
	if !ok || s.Workloads == nil {
		writeError(w, http.StatusServiceUnavailable, "update Iglo-OS to manage gaming")
		return
	}
	var input struct {
		Play *bool `json:"play"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&input) != nil || input.Play == nil {
		writeError(w, http.StatusBadRequest, "give play true or false")
		return
	}
	if !*input.Play {
		if err := a.SetGamingPower(r.Context(), false); err != nil {
			s.writeAppsResult(w, err, 0)
			return
		}
		s.writeWorkloadsResult(w, s.Workloads.Protect(0))
		return
	}
	// Defer heavy work first, so it doesn't compete with the game that starts.
	wasProtected := s.Workloads.Status().GamingActive
	if err := s.Workloads.Protect(playMinutes); err != nil {
		s.writeWorkloadsResult(w, err)
		return
	}
	if err := a.SetGamingPower(r.Context(), true); err != nil {
		if !wasProtected {
			s.Workloads.Protect(0)
		}
		s.writeAppsResult(w, err, 0)
		return
	}
	s.Logger.Info("gaming session started")
	w.WriteHeader(http.StatusNoContent)
}
