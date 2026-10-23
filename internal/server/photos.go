package server

import (
	"context"
	"iglo/internal/agent"
	"net/http"
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
