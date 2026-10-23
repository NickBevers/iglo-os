package apps

import (
	"context"
	"errors"
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
	// Protected is set while gaming protection defers heavy background work.
	Protected      bool      `json:"protected"`
	ProtectedUntil time.Time `json:"protectedUntil,omitzero"`
}

type guestActions interface {
	RunGuestAction(ctx context.Context, node string, guestType proxmox.GuestType, vmid int, action proxmox.GuestAction) (string, error)
}

func (s *Service) Gaming(ctx context.Context) (GamingView, error) {
	view := GamingView{Library: []agent.Figure{}}
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
