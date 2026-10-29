package workloads

import (
	"context"
	"errors"
	"time"

	"iglo/internal/immich"
)

// ConnectInstalled connects a Photos that Iglo-OS just installed at address.
// It makes the first Immich admin, then a key that may only read and pause
// the queues, and saves it with the node of the host. It turns control on;
// the other settings stay as they are.
//
// A connection that Nick made is never replaced. When Background work has a
// key or pause ownership, or another server, only the admin is made.
func (s *Service) ConnectInstalled(ctx context.Context, address string, admin immich.Admin) error {
	if err := admin.Validate(); err != nil {
		return err
	}
	client := immich.Client{HTTP: s.http, Server: address}
	token, err := client.CreateAdmin(ctx, admin)
	if err != nil {
		return err
	}
	if !s.free(address) {
		return nil
	}
	key, err := client.QueueKey(ctx, token)
	if err != nil {
		return err
	}
	// The memory checks read the host of the agent, so that is the node.
	// Without its answer the key is saved and control stays off, so Nick only
	// has to choose the node.
	node := ""
	if s.hostMemory != nil {
		memoryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if memory, err := s.hostMemory(memoryCtx); err == nil {
			node = memory.Node
		}
		cancel()
	}

	s.op.Lock()
	defer s.op.Unlock()
	if !s.freeLocked(address) {
		return nil
	}
	old := s.state.Settings
	settings := old
	settings.Queues = append([]string(nil), old.Queues...)
	settings.URL, settings.APIKey, settings.Node, settings.Enabled = address, key, node, node != ""
	if err := settings.Validate(); err != nil {
		return err
	}
	s.state.Settings = settings
	if err := s.save(); err != nil {
		s.state.Settings = old
		return err
	}
	view := s.Status()
	view.Reason, view.Error = "Applying settings", ""
	s.publish(view)
	s.signal()
	if node == "" {
		return errors.New("connected Immich, but the host agent did not say its node, so control is off")
	}
	return nil
}

// free says whether Background work may still connect to address: it has no
// key, owns no pauses, and has no other server.
func (s *Service) free(address string) bool {
	s.op.Lock()
	defer s.op.Unlock()
	return s.freeLocked(address)
}

func (s *Service) freeLocked(address string) bool {
	settings := s.state.Settings
	return settings.APIKey == "" && len(s.state.Owned) == 0 && (settings.URL == "" || settings.URL == address)
}
