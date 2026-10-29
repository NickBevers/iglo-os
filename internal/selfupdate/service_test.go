package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"iglo/internal/agent"
)

type fakeAgent struct {
	status agent.UpgradeStatus
	err    error
}

func (f fakeAgent) UpgradeStatus(context.Context) (agent.UpgradeStatus, error) {
	return f.status, f.err
}

func TestStatusShowsTheVersionAndTheLastUpgrade(t *testing.T) {
	dir := t.TempDir()
	// An old GitHub settings file, maybe with a token, goes away.
	old := filepath.Join(dir, "self-update.json")
	os.WriteFile(old, []byte(`{"settings":{"token":"secret"}}`), 0o600)

	service, err := New(Options{DataDir: dir, Version: "v1.2.3", Agent: fakeAgent{status: agent.UpgradeStatus{State: agent.UpgradeSucceeded, Version: "v1.2.3", Log: "==> Done\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old settings file: %v", err)
	}
	view := service.Status(context.Background())
	if view.Version != "v1.2.3" || view.Upgrade == nil || view.Upgrade.Log != "==> Done" {
		t.Fatalf("view = %+v", view)
	}

	service.Agent = fakeAgent{status: agent.UpgradeStatus{State: agent.UpgradeIdle}}
	if view := service.Status(context.Background()); view.Upgrade != nil {
		t.Fatalf("idle upgrade shown: %+v", view.Upgrade)
	}
	service.Agent = fakeAgent{err: errors.New("offline")}
	if view := service.Status(context.Background()); view.Version != "v1.2.3" || view.Upgrade != nil {
		t.Fatalf("offline agent: %+v", view)
	}
}
