package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"iglo/internal/agent"
	"iglo/internal/release"
)

func TestAdminSocketIsRootOnly(t *testing.T) {
	path := filepath.Join(socketDir(t), "admin", "admin.sock")
	listener, err := listenAdmin(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for file, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %v (%v)", file, info.Mode().Perm(), want, err)
		}
	}
}

// fakeUpgrade answers like the agent: it accepts the bundle, then reports a
// growing log, a restart in between, and the result.
func fakeUpgrade(t *testing.T, final agent.UpgradeState) (string, *bytes.Buffer) {
	t.Helper()
	path := filepath.Join(socketDir(t), "admin.sock")
	listener, err := listenAdmin(path)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	received := &bytes.Buffer{}
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/upgrade", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if _, err := agent.DecodeSignatureHeader(r.Header.Get(agent.SignatureHeader)); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received.ReadFrom(r.Body)
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /v1/upgrade", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		status := agent.UpgradeStatus{Version: "v1.2.3", State: agent.UpgradeRunning, Log: "==> Saving the current version\n"}
		switch {
		case call == 2:
			// The agent restarts here, so the call fails.
			http.Error(w, "restarting", http.StatusBadGateway)
			return
		case call >= 3:
			status.State, status.Log, status.Message = final, status.Log+"==> Installing the new version\n", "the new version did not start"
		}
		json.NewEncoder(w).Encode(status)
	})
	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return path, received
}

func writeBundle(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bundle, sig := filepath.Join(dir, "iglo.tar.gz"), filepath.Join(dir, "iglo.tar.gz.sig")
	os.WriteFile(bundle, []byte("bundle bytes"), 0o644)
	data, _ := json.Marshal(release.Signature{Version: "v1.2.3", SHA256: strings.Repeat("ab", 32), Signature: "c2lnbmF0dXJl"})
	os.WriteFile(sig, data, 0o644)
	return bundle, sig
}

func TestUpgradeCommandFollowsTheUpgrade(t *testing.T) {
	socket, received := fakeUpgrade(t, agent.UpgradeSucceeded)
	bundle, sig := writeBundle(t)
	out := &bytes.Buffer{}
	client := agent.NewClient(socket)
	if err := client.StartUpgrade(context.Background(), mustOpen(t, bundle), mustSignature(t, sig)); err != nil {
		t.Fatal(err)
	}
	if err := followUpgrade(context.Background(), client, "v1.2.3", out, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if received.String() != "bundle bytes" {
		t.Errorf("agent got %q", received.String())
	}
	// Each log line shows once, also across the restart.
	want := "==> Saving the current version\n==> Installing the new version\nUpgraded to v1.2.3\n"
	if out.String() != want {
		t.Errorf("output = %q", out.String())
	}
}

func TestUpgradeCommandReportsAFailure(t *testing.T) {
	socket, _ := fakeUpgrade(t, agent.UpgradeFailed)
	bundle, sig := writeBundle(t)
	out := &bytes.Buffer{}
	err := upgradeCommand([]string{"-socket", socket, "-wait", "5s", bundle, sig}, out)
	if err == nil || !strings.Contains(err.Error(), "the new version did not start") {
		t.Fatalf("err = %v, output %q", err, out.String())
	}
	if err := upgradeCommand([]string{bundle}, out); err == nil {
		t.Fatal("accepted a missing signature")
	}
}

// socketDir is a short folder: macOS allows only about 104 bytes for a
// socket path, and t.TempDir() is longer.
func socketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ia")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func mustSignature(t *testing.T, path string) release.Signature {
	t.Helper()
	data, _ := os.ReadFile(path)
	var signature release.Signature
	if err := json.Unmarshal(data, &signature); err != nil {
		t.Fatal(err)
	}
	return signature
}
