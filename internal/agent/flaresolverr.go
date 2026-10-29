package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// FlareSolverrView says whether the media stack runs FlareSolverr.
type FlareSolverrView struct {
	Enabled bool `json:"enabled"`
}

// FlareSolverrRequest turns FlareSolverr of the media stack in container VMID on or off.
type FlareSolverrRequest struct {
	VMID    int  `json:"vmid"`
	Enabled bool `json:"enabled"`
}

// FlareSolverr reads the FlareSolverr answer in .env of the media stack in
// container vmid. A stack from before the switch has none, and runs it.
func (i *AppInstaller) FlareSolverr(ctx context.Context, vmid int) (FlareSolverrView, error) {
	if vmid < 100 || vmid > 999999999 {
		return FlareSolverrView{}, ErrInvalidContainer
	}

	output, err := i.output(ctx, "pct", "exec", strconv.Itoa(vmid), "--", "sed", "-n", "s/^FLARESOLVERR=//p", "/opt/arr/.env")
	if err != nil {
		return FlareSolverrView{}, fmt.Errorf("could not read the FlareSolverr setting of container %d: %w", vmid, err)
	}

	return FlareSolverrView{Enabled: strings.TrimSpace(string(output)) != "n"}, nil
}

// ChangeFlareSolverr turns FlareSolverr of the media stack in container vmid
// on or off. It needs no answers file: the installer only gets on or off.
func (i *AppInstaller) ChangeFlareSolverr(vmid int, enabled bool) error {
	if vmid < 100 || vmid > 999999999 {
		return ErrInvalidContainer
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	script, err := i.prepare(MediaStackApp)
	if err != nil {
		return err
	}
	state := "off"
	if enabled {
		state = "on"
	}

	return i.run(MediaStackApp, ActionFlareSolverr, script, []string{"--flaresolverr", state, "--ctid", strconv.Itoa(vmid)}, "")
}
