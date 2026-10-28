package agent

import (
	"encoding/json"
	"errors"
	"net/http"
)

func RegisterHeadscale(mux *http.ServeMux, i *AppInstaller) {
	mux.HandleFunc("POST /v1/apps/headscale/remove", func(w http.ResponseWriter, r *http.Request) {
		var in AppContainerRequest
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&in) != nil {
			http.Error(w, "invalid confirmation", 400)
			return
		}
		if err := i.RemoveHeadscale(r.Context(), in.VMID, in.Confirm); err != nil {
			status := http.StatusBadGateway
			if errors.Is(err, ErrInvalidAnswers) || errors.Is(err, ErrInvalidContainer) {
				status = 400
			}
			if errors.Is(err, ErrAppInstallRunning) {
				status = 409
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.WriteHeader(202)
	})
}
