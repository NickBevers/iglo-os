package apps

import (
	"context"
	"errors"
	"slices"
	"time"

	"iglo/internal/agent"
	"iglo/internal/proxmox"
)

// GamingView is the state of the Gaming app for its page.
type GamingView struct {
	Installed bool   `json:"installed"`
	VMID      int    `json:"vmid,omitempty"`
	Status    string `json:"status,omitempty"`
	// Address is the IP that you add as a host in Moonlight, while the container runs.
	Address string `json:"address,omitempty"`
	// Library is the number of games per system on the Games share.
	Library      []agent.Figure `json:"library"`
	LibraryError string         `json:"libraryError,omitempty"`
	// Sample marks the made-up library of the dev mock and the demo.
	Sample bool `json:"sample,omitempty"`
	// Sessions are the Moonlight streams that run now. There is one at most,
	// because every screen shares the same saves.
	Sessions []agent.GamingSession `json:"sessions"`
	// Protected is set while gaming protection defers heavy background work.
	Protected      bool      `json:"protected"`
	ProtectedUntil time.Time `json:"protectedUntil,omitzero"`
}

type guestActions interface {
	RunGuestAction(ctx context.Context, node string, guestType proxmox.GuestType, vmid int, action proxmox.GuestAction) (string, error)
}

func (s *Service) Gaming(ctx context.Context) (GamingView, error) {
	view := GamingView{Library: []agent.Figure{}, Sessions: []agent.GamingSession{}}
	_, guest, err := s.managedGuest(ctx, agent.GamingApp)
	if errors.Is(err, ErrNotInstalled) {
		return view, nil
	}
	if err != nil {
		return view, err
	}
	view.Installed, view.VMID, view.Status = true, guest.VMID, guest.Status
	if guest.Status == "running" {
		view.Address = s.guestIP(ctx, guest)
		s.gamingMu.Lock()
		view.Sessions = append(view.Sessions, s.gamingSessions...)
		s.gamingMu.Unlock()
	}
	stats, err := s.Agent.AppStats(ctx, agent.GamingApp, guest.VMID)
	if err != nil {
		view.LibraryError = err.Error()
		return view, nil
	}
	view.Library, view.Sample = stats.Figures, stats.Sample
	return view, nil
}

// SetGamingPower starts the gaming container for a session, or shuts it down
// after one. It does nothing when the container already is that way.
func (s *Service) SetGamingPower(ctx context.Context, on bool) error {
	_, guest, err := s.managedGuest(ctx, agent.GamingApp)
	if err != nil {
		return err
	}
	if (guest.Status == "running") == on {
		return nil
	}
	if err := s.checkIdle(ctx); err != nil {
		return err
	}
	actions, ok := s.Proxmox.(guestActions)
	if !ok {
		return agent.ErrAppNotAvailable
	}
	action := proxmox.Shutdown
	if on {
		action = proxmox.Start
	}
	_, err = actions.RunGuestAction(ctx, guest.Node, proxmox.LXC, guest.VMID, action)
	return err
}

type gamingAgent interface {
	GamingGames(ctx context.Context, vmid int) ([]agent.GameSystem, error)
	GamingCover(ctx context.Context, vmid int, system, name string) ([]byte, string, error)
}

// GamingGames lists the games per system on the Games share.
func (s *Service) GamingGames(ctx context.Context) ([]agent.GameSystem, error) {
	_, guest, err := s.managedGuest(ctx, agent.GamingApp)
	if err != nil {
		return nil, err
	}
	a, ok := s.Agent.(gamingAgent)
	if !ok {
		return nil, agent.ErrAppNotAvailable
	}
	return a.GamingGames(ctx, guest.VMID)
}

// GamingOwnCover returns the cover that you put on the Games share for a
// game, or agent.ErrNoCover.
func (s *Service) GamingOwnCover(ctx context.Context, system, name string) ([]byte, string, error) {
	_, guest, err := s.managedGuest(ctx, agent.GamingApp)
	if err != nil {
		return nil, "", err
	}
	a, ok := s.Agent.(gamingAgent)
	if !ok {
		return nil, "", agent.ErrNoCover
	}
	return a.GamingCover(ctx, guest.VMID, system, name)
}

type gamingSessionAgent interface {
	GamingSessions(ctx context.Context, vmid int) ([]agent.GamingSession, error)
	StopGamingSession(ctx context.Context, vmid int, id string) error
}

const (
	// gamingCheck is how often WatchGaming looks for Moonlight sessions while
	// the gaming container runs. A second session stops within this time.
	gamingCheck = 10 * time.Second
	// gamingIdleCheck is how often it looks while the container is stopped.
	gamingIdleCheck = 30 * time.Second
)

// WatchGaming follows the Moonlight sessions of the gaming container. While
// one runs, hold keeps heavy background work deferred, also when the container
// was started without Play. Every screen shares the same saves, so a session
// of a second device stops before its emulator can write them.
func (s *Service) WatchGaming(ctx context.Context, hold func() error) {
	for {
		wait := s.checkGaming(ctx, hold)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (s *Service) checkGaming(ctx context.Context, hold func() error) time.Duration {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	a, ok := s.Agent.(gamingSessionAgent)
	if !ok {
		return gamingIdleCheck
	}
	_, guest, err := s.managedGuest(ctx, agent.GamingApp)
	if err != nil || guest.Status != "running" {
		s.setGamingSessions(nil, "")
		return gamingIdleCheck
	}
	sessions, err := a.GamingSessions(ctx, guest.VMID)
	if err != nil {
		// Wolf may still be starting. A running hold covers a short gap.
		s.Logger.Debug("gaming sessions", "error", err)
		s.setGamingSessions(nil, "")
		return gamingCheck
	}
	s.gamingMu.Lock()
	owner := s.gamingOwner
	s.gamingMu.Unlock()
	// Wolf moves a resumed session to the end of its list, so the first one
	// seen stays the owner. Sessions that are new together go by list order.
	if !slices.ContainsFunc(sessions, func(session agent.GamingSession) bool { return session.ID == owner }) {
		owner = ""
		if len(sessions) > 0 {
			owner = sessions[0].ID
		}
	}
	kept := []agent.GamingSession{}
	for _, session := range sessions {
		if session.ID == owner {
			kept = append(kept, session)
			continue
		}
		if err := a.StopGamingSession(ctx, guest.VMID, session.ID); err != nil {
			s.Logger.Warn("could not stop a second gaming session", "client", session.ClientIP, "error", err)
			kept = append(kept, session)
			continue
		}
		s.Logger.Warn("stopped a second gaming session; play on one screen at a time", "client", session.ClientIP)
	}
	if len(kept) > 0 {
		if err := hold(); err != nil {
			s.Logger.Warn("could not hold background work for a gaming session", "error", err)
		}
	}
	s.setGamingSessions(kept, owner)
	return gamingCheck
}

func (s *Service) setGamingSessions(sessions []agent.GamingSession, owner string) {
	s.gamingMu.Lock()
	defer s.gamingMu.Unlock()
	s.gamingSessions, s.gamingOwner = sessions, owner
}

// StopGamingSession ends a Moonlight session, like one that a device left
// running when it disconnected without quitting.
func (s *Service) StopGamingSession(ctx context.Context, id string) error {
	_, guest, err := s.managedGuest(ctx, agent.GamingApp)
	if err != nil {
		return err
	}
	a, ok := s.Agent.(gamingSessionAgent)
	if !ok {
		return agent.ErrAppNotAvailable
	}
	if err := a.StopGamingSession(ctx, guest.VMID, id); err != nil {
		return err
	}
	s.gamingMu.Lock()
	defer s.gamingMu.Unlock()
	s.gamingSessions = slices.DeleteFunc(slices.Clone(s.gamingSessions), func(session agent.GamingSession) bool { return session.ID == id })
	return nil
}
