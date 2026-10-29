// Package selfupdate shows which version of Iglo-OS runs, and how its last
// upgrade went. Upgrades come from the owner's own machine with
// "make deploy", straight to the host agent's root-only socket; the manager
// can't start one. See docs/development.md.
package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"iglo/internal/agent"
)

type Agent interface {
	UpgradeStatus(ctx context.Context) (agent.UpgradeStatus, error)
}

type Options struct {
	DataDir string
	// Version is the running version.
	Version string
	Agent   Agent
}

type View struct {
	Version string               `json:"version"`
	Upgrade *agent.UpgradeStatus `json:"upgrade"`
}

type Service struct {
	Options
}

func New(options Options) (*Service, error) {
	// Builds before make deploy kept GitHub settings here, maybe with a token.
	if options.DataDir != "" {
		if err := os.Remove(filepath.Join(options.DataDir, "self-update.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return &Service{Options: options}, nil
}

func (s *Service) Status(ctx context.Context) View {
	view := View{Version: s.Version}
	if upgrade, err := s.Agent.UpgradeStatus(ctx); err == nil && upgrade.State != agent.UpgradeIdle {
		upgrade.Log = strings.TrimSpace(upgrade.Log)
		view.Upgrade = &upgrade
	}
	return view
}
