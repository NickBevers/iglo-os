package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"iglo/internal/health"
)

// ready says whether this manager works: it reaches the host agent and
// Proxmox. An upgrade waits for it before it keeps a new version. It answers
// only on the loopback address and shows nothing but yes or no.
func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	if ip := net.ParseIP(clientIP(r)); ip == nil || !ip.IsLoopback() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if _, err := s.Agent.Health(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "the host agent does not answer")
		return
	}
	if _, err := s.Proxmox.Resources(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "Proxmox does not answer")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ready": true})
}

func (s *server) healthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Health.Status())
}

// healthRefresh checks everything now and returns the new status.
func (s *server) healthRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()

	var wait sync.WaitGroup
	wait.Go(func() { s.Health.CheckSystem(ctx) })
	wait.Go(func() { s.Health.CheckServices(ctx) })
	wait.Wait()

	writeJSON(w, http.StatusOK, s.Health.Status())
}

func (s *server) healthAddCheck(w http.ResponseWriter, r *http.Request) {
	input, ok := readCheckInput(w, r)
	if !ok {
		return
	}

	check, err := s.Health.AddCheck(input)
	if err != nil {
		s.writeCheckError(w, err)
		return
	}

	// Check right away, so the new service doesn't wait a minute for its status.
	go s.Health.CheckServices(s.Background)
	writeJSON(w, http.StatusCreated, check)
}

func (s *server) healthUpdateCheck(w http.ResponseWriter, r *http.Request) {
	input, ok := readCheckInput(w, r)
	if !ok {
		return
	}

	if err := s.Health.UpdateCheck(r.PathValue("id"), input); err != nil {
		s.writeCheckError(w, err)
		return
	}

	go s.Health.CheckServices(s.Background)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) healthDeleteCheck(w http.ResponseWriter, r *http.Request) {
	if err := s.Health.DeleteCheck(r.PathValue("id")); err != nil {
		s.writeCheckError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func readCheckInput(w http.ResponseWriter, r *http.Request) (health.CheckInput, bool) {
	var input health.CheckInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return input, false
	}

	return input, true
}

func (s *server) writeCheckError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, health.ErrInvalidCheck):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, health.ErrCheckNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		s.Logger.Error("save service check", "error", err)
		writeError(w, http.StatusInternalServerError, "could not save the check")
	}
}
