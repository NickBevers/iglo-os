package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"iglo/internal/agent"
	"iglo/internal/apps"
	"iglo/internal/covers"
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

type gamingLibrary interface {
	GamingGames(context.Context) ([]agent.GameSystem, error)
	GamingOwnCover(ctx context.Context, system, name string) ([]byte, string, error)
}

func (s *server) gamingGames(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Apps.(gamingLibrary)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "update Iglo-OS to see the game library")
		return
	}
	systems, err := a.GamingGames(r.Context())
	if err != nil {
		s.writeAppsResult(w, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"systems": systems})
}

// gamingCover serves your own cover from the Games share, and otherwise the
// box art from libretro. Without either, the page shows a placeholder. The
// game list marks own covers, and the page asks for them with own=1, so other
// covers skip the lookup of the container.
func (s *server) gamingCover(w http.ResponseWriter, r *http.Request) {
	system, name := r.PathValue("system"), r.PathValue("name")
	if !agent.ValidGame(system, name) {
		writeError(w, http.StatusBadRequest, "invalid game")
		return
	}
	if a, ok := s.Apps.(gamingLibrary); ok && r.URL.Query().Get("own") == "1" {
		data, contentType, err := a.GamingOwnCover(r.Context(), system, name)
		if err == nil {
			writeCover(w, contentType, data)
			return
		}
		if !errors.Is(err, agent.ErrNoCover) {
			s.Logger.Warn("own game cover", "system", system, "error", err)
		}
	}
	if s.Covers != nil {
		if data, err := s.Covers.Cover(r.Context(), system, name); err == nil {
			writeCover(w, covers.ContentType, data)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
}

func writeCover(w http.ResponseWriter, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(data)
}
