package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"iglo/internal/agent"
	"iglo/internal/release"
)

// defaultAdminSocket is the socket for upgrades from the host itself. Only
// root may use it, and no container mounts its folder, so the manager can't
// reach it.
const defaultAdminSocket = "/run/iglo-agent/admin.sock"

// listenAdmin opens the root-only socket.
func listenAdmin(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

// adminHandler serves the upgrade on the root-only socket.
func adminHandler(upgrader *agent.Upgrader, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", health)
	mux.HandleFunc("POST /v1/upgrade", startUpgrade(upgrader, logger))
	mux.HandleFunc("GET /v1/upgrade", upgradeStatus(upgrader))
	return mux
}

// upgradeCommand is "iglo-agent upgrade BUNDLE SIGNATURE": it hands a signed
// bundle to the running agent and prints the install log until it is done.
// The agent checks the signature and the version, as for any upgrade.
func upgradeCommand(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	socket := flags.String("socket", defaultAdminSocket, "the agent's root-only socket")
	wait := flags.Duration("wait", 10*time.Minute, "how long to follow the upgrade")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return errors.New("usage: iglo-agent upgrade BUNDLE SIGNATURE")
	}

	data, err := os.ReadFile(flags.Arg(1))
	if err != nil {
		return err
	}
	var signature release.Signature
	if err := json.Unmarshal(data, &signature); err != nil {
		return fmt.Errorf("%s is not a signature: %w", flags.Arg(1), err)
	}
	bundle, err := os.Open(flags.Arg(0))
	if err != nil {
		return err
	}
	defer bundle.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *wait)
	defer cancel()
	client := agent.NewClient(*socket)
	if err := client.StartUpgrade(ctx, bundle, signature); err != nil {
		return err
	}
	fmt.Fprintf(out, "Upgrading to %s\n", signature.Version)
	return followUpgrade(ctx, client, signature.Version, out, 2*time.Second)
}

// followUpgrade prints the new lines of the upgrade log until the upgrade
// ends. The agent restarts during an upgrade, so failed calls are retried
// until ctx ends.
func followUpgrade(ctx context.Context, client *agent.Client, version string, out io.Writer, every time.Duration) error {
	shown := ""
	for {
		status, err := client.UpgradeStatus(ctx)
		if err == nil {
			if strings.HasPrefix(status.Log, shown) {
				io.WriteString(out, status.Log[len(shown):])
			} else {
				io.WriteString(out, status.Log)
			}
			shown = status.Log
			if status.Version == version && status.State == agent.UpgradeSucceeded {
				fmt.Fprintf(out, "Upgraded to %s\n", version)
				return nil
			}
			if status.Version == version && status.State == agent.UpgradeFailed {
				return fmt.Errorf("the upgrade failed: %s", status.Message)
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("the upgrade did not finish in time; follow it with: journalctl -u iglo-upgrade")
		case <-time.After(every):
		}
	}
}
