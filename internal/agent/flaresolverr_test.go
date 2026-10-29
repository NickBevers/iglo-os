package agent

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
)

func TestChangeFlareSolverr(t *testing.T) {
	installer, unit := newTestInstaller(t)

	if err := installer.ChangeFlareSolverr(1, true); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("vmid 1: %v", err)
	}
	if err := installer.ChangeFlareSolverr(130, true); err != nil {
		t.Fatal(err)
	}
	// No answers file: the installer only gets on or off.
	if !slices.Equal(unit.args, []string{"--flaresolverr", "on", "--ctid", "130"}) || unit.started[1] != "" {
		t.Fatalf("args = %v, cleanup = %q", unit.args, unit.started[1])
	}
	if status := installer.Status(); status.Action != ActionFlareSolverr || status.App != MediaStackApp || status.State != UpgradeRunning {
		t.Fatalf("status = %+v", status)
	}
	if err := installer.ChangeFlareSolverr(130, false); !errors.Is(err, ErrAppInstallRunning) {
		t.Fatalf("second change while one runs: %v", err)
	}

	unit.running = false
	os.WriteFile(installer.exitPath(), []byte("1\n"), 0o600)
	if status := installer.Status(); status.State != UpgradeFailed || status.Message != "the FlareSolverr change stopped with exit code 1" {
		t.Fatalf("failed status = %+v", status)
	}
	if err := installer.ChangeFlareSolverr(130, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(unit.args, []string{"--flaresolverr", "off", "--ctid", "130"}) {
		t.Fatalf("args = %v", unit.args)
	}
}

func TestFlareSolverrView(t *testing.T) {
	installer, _ := newTestInstaller(t)
	var command []string
	output := "n\n"
	installer.output = func(_ context.Context, name string, args ...string) ([]byte, error) {
		command = append([]string{name}, args...)
		return []byte(output), nil
	}

	if view, err := installer.FlareSolverr(context.Background(), 130); err != nil || view.Enabled {
		t.Fatalf("off: %+v, %v", view, err)
	}
	if !slices.Equal(command[:5], []string{"pct", "exec", "130", "--", "sed"}) || command[len(command)-1] != "/opt/arr/.env" {
		t.Fatalf("command = %v", command)
	}
	output = "y\n"
	if view, _ := installer.FlareSolverr(context.Background(), 130); !view.Enabled {
		t.Fatal("on reads as off")
	}
	// A stack from before the switch has no answer, and runs it.
	output = ""
	if view, _ := installer.FlareSolverr(context.Background(), 130); !view.Enabled {
		t.Fatal("an old stack reads as off")
	}
	if _, err := installer.FlareSolverr(context.Background(), 1); !errors.Is(err, ErrInvalidContainer) {
		t.Fatalf("vmid 1: %v", err)
	}
}
