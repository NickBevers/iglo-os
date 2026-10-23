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
