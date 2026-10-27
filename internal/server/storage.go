package server

import (
	"context"
	"encoding/json"
	"iglo/internal/agent"
	"net/http"
)

type storageAgent interface {
	StorageBindings(context.Context) (agent.BindingView, error)
	CreateBinding(context.Context, agent.BindingInput) error
	DeleteBinding(context.Context, string) error
}

func (s *server) storageBindings(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Agent.(storageAgent)
	if !ok {
		writeError(w, 503, "update the host agent to manage storage bindings")
		return
	}
	if r.Method == http.MethodGet {
		view, err := a.StorageBindings(r.Context())
		if err != nil {
			writeError(w, 502, err.Error())
			return
		}
		writeJSON(w, 200, view)
		return
	}
	var err error
	if r.Method == http.MethodPost {
		var in agent.BindingInput
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			writeError(w, 400, "invalid request")
			return
		}
		if err = in.Validate(); err == nil {
			err = a.CreateBinding(r.Context(), in)
		}
	} else {
		err = a.DeleteBinding(r.Context(), r.PathValue("id"))
	}
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	w.WriteHeader(204)
}

type backupStorageAgent interface {
	UseBindingForBackups(ctx context.Context, id string, on bool) error
}

// bindingBackups turns the Proxmox backups on a Backups share on or off.
func (s *server) bindingBackups(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Agent.(backupStorageAgent)
	if !ok {
		writeError(w, 503, "update the host agent to keep Proxmox backups on a share")
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&in) != nil || in.Enabled == nil {
		writeError(w, 400, "give enabled true or false")
		return
	}
	if err := a.UseBindingForBackups(r.Context(), r.PathValue("id"), *in.Enabled); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	w.WriteHeader(204)
}
