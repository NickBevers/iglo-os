package server

import (
	"context"
	"encoding/json"
	"net/http"

	"iglo/internal/agent"
)

func (s *server) photosRecovery(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Apps.(interface {
		RecoverPhotos(context.Context, context.Context, agent.AppAction) error
	})
	if !ok {
		writeError(w, 503, "update Iglo-OS to run Photos recovery checks")
		return
	}
	s.writeAppsResult(w, a.RecoverPhotos(r.Context(), s.Background, agent.AppAction(r.PathValue("action"))), http.StatusAccepted)
}

// photosBackups saves the settings of the Photos backups: the time of the
// nightly backup, the retention and the folder off the NAS.
func (s *server) photosBackups(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Apps.(interface {
		SavePhotosBackups(context.Context, agent.PhotosBackupSettings) error
	})
	if !ok {
		writeError(w, 503, "update Iglo-OS to change the Photos backups")
		return
	}
	var settings agent.PhotosBackupSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.writeAppsResult(w, a.SavePhotosBackups(r.Context(), settings), http.StatusNoContent)
}
