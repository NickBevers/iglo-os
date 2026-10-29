package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"iglo/internal/agent"
	"iglo/internal/apps"
	"iglo/internal/immich"
)

func (s *server) appsStatus(w http.ResponseWriter, r *http.Request) {
	view, err := s.Apps.Status(r.Context())
	if err != nil {
		s.Logger.Error("apps status", "error", err)
		writeError(w, http.StatusBadGateway, "could not reach Proxmox")
		return
	}

	writeJSON(w, http.StatusOK, view)
}

func (s *server) appsInstall(w http.ResponseWriter, r *http.Request) {
	var request struct {
		agent.InstallRequest
		// Immich is the first admin of a new Photos. It stays in the memory
		// of the manager and never reaches the host agent.
		Immich *immich.Admin `json:"immich"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := r.PathValue("id")
	if request.Immich == nil {
		s.writeAppsResult(w, s.Apps.Install(r.Context(), s.Background, id, request.InstallRequest), http.StatusAccepted)
		return
	}
	switch {
	case id != agent.PhotosApp:
		writeError(w, http.StatusBadRequest, "only Photos makes an Immich admin")
		return
	case s.ConnectPhotos == nil:
		writeError(w, http.StatusServiceUnavailable, "this manager can't make an Immich admin")
		return
	}
	admin := *request.Immich
	if err := admin.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	then := func(ctx context.Context, status agent.AppInstallStatus) error {
		return s.ConnectPhotos(ctx, status, admin)
	}
	s.writeAppsResult(w, s.Apps.InstallThen(r.Context(), s.Background, id, request.InstallRequest, then), http.StatusAccepted)
}

func (s *server) appsRetry(w http.ResponseWriter, r *http.Request) {
	s.writeAppsResult(w, s.Apps.Retry(r.Context(), s.Background, r.PathValue("id")), http.StatusAccepted)
}

func (s *server) appsForget(w http.ResponseWriter, r *http.Request) {
	s.writeAppsResult(w, s.Apps.Forget(r.Context(), r.PathValue("id")), http.StatusNoContent)
}

func (s *server) appsUpdate(w http.ResponseWriter, r *http.Request) {
	s.writeAppsResult(w, s.Apps.Update(r.Context(), s.Background, r.PathValue("id")), http.StatusAccepted)
}

func (s *server) appsRemove(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("id") == agent.HeadscaleApp {
		var in struct {
			Confirm string `json:"confirm"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil {
			writeError(w, 400, "confirm the controller hostname")
			return
		}
		a, ok := s.Apps.(interface {
			RemoveHeadscale(context.Context, context.Context, string) error
		})
		if !ok {
			writeError(w, 503, "update Iglo-OS first")
			return
		}
		s.writeAppsResult(w, a.RemoveHeadscale(r.Context(), s.Background, in.Confirm), 202)
		return
	}
	s.writeAppsResult(w, s.Apps.Remove(r.Context(), s.Background, r.PathValue("id")), http.StatusAccepted)
}

func (s *server) appsVPN(w http.ResponseWriter, r *http.Request) {
	view, err := s.Apps.VPN(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeAppsResult(w, err, 0)
		return
	}

	writeJSON(w, http.StatusOK, view)
}

func (s *server) appsChangeVPN(w http.ResponseWriter, r *http.Request) {
	var settings agent.VPNSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeAppsResult(w, s.Apps.ChangeVPN(r.Context(), s.Background, r.PathValue("id"), settings), http.StatusAccepted)
}

// appsChangeFlareSolverr turns FlareSolverr of the media stack on or off.
func (s *server) appsChangeFlareSolverr(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&request); err != nil || request.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.writeAppsResult(w, s.Apps.ChangeFlareSolverr(r.Context(), s.Background, r.PathValue("id"), *request.Enabled), http.StatusAccepted)
}

func (s *server) writeAppsResult(w http.ResponseWriter, err error, success int) {
	switch {
	case errors.Is(err, agent.ErrInvalidAnswers):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, apps.ErrUnknownApp):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, apps.ErrInstalled), errors.Is(err, apps.ErrBusy), errors.Is(err, apps.ErrNotInstalled), errors.Is(err, apps.ErrPlanned),
		errors.Is(err, apps.ErrNotRunning):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.Logger.Error("app operation", "error", err)
		// The agent's message says what to do, like "update Iglo-OS first".
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		w.WriteHeader(success)
	}
}
