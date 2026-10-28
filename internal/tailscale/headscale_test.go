package tailscale

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestControlURLValidation(t *testing.T) {
	for _, value := range []string{"http://headscale.example", "https://user:secret@headscale.example", "https://headscale.example/path", "https://headscale.example?key=secret", "https://headscale.example#fragment", "https://headscale.example?", "https://headscale.example#", "https://headscale.example:0", "https://headscale.example:99999", "https:///", "file:/secret"} {
		if (Settings{LoginServer: value}).Validate() == nil {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"", "https://headscale.example", "https://headscale.example:8443", "https://[fd7a::1]:8443"} {
		if err := (Settings{LoginServer: value}).Validate(); err != nil {
			t.Fatal(value, err)
		}
	}
}
func TestControlServerRequiresLogoutAndDropsOldTag(t *testing.T) {
	cli := &fakeCLI{outputs: map[string]string{"status --json": runningStatus}}
	s := newTestService(t, cli)
	s.settings.UseTag = true
	if err := s.SaveSettings(context.Background(), Settings{LoginServer: "https://headscale.example"}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatal(err)
	}
	if s.settings.LoginServer != "" {
		t.Fatal("changed active controller")
	}
	cli.outputs["status --json"] = `{"BackendState":"Stopped"}`
	if err := s.SaveSettings(context.Background(), Settings{LoginServer: "https://headscale.example"}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatal("accepted stopped, still authenticated client", err)
	}
	cli.outputs["status --json"] = `{"BackendState":"NeedsLogin"}`
	if err := s.SaveSettings(context.Background(), Settings{LoginServer: " https://headscale.example/ "}); err != nil {
		t.Fatal(err)
	}
	if s.settings.LoginServer != "https://headscale.example" || s.settings.UseTag {
		t.Fatal(s.settings)
	}
	s.connecting = true
	if err := s.SaveSettings(context.Background(), Settings{}); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
}
func TestHeadscaleSkipsHostedFeaturesButKeepsPeers(t *testing.T) {
	cli := &fakeCLI{outputs: map[string]string{"status --json": runningStatus, "serve status": `{"Services":{"svc:jellyfin":{}}}`}}
	s := newTestService(t, cli)
	s.settings.LoginServer = "https://headscale.example"
	v := s.Status(context.Background())
	if len(v.Peers) != 2 || v.Serving || len(v.Services) != 0 || cli.called("serve") != nil {
		t.Fatal(v, cli.calls)
	}
	if err := s.SetServe(context.Background(), true); !errors.Is(err, ErrInvalidSettings) {
		t.Fatal(err)
	}
	if err := s.SetService(context.Background(), "jellyfin", true); !errors.Is(err, ErrInvalidSettings) {
		t.Fatal(err)
	}
	if err := s.UseTag(context.Background()); !errors.Is(err, ErrInvalidSettings) {
		t.Fatal(err)
	}
	if cli.called("serve") != nil || cli.called("up") != nil {
		t.Fatal(cli.calls)
	}
}
func TestHeadscaleLoginKeyIsEphemeralAndNotEchoed(t *testing.T) {
	key := "headscale-fixture-secret"
	cli := &fakeCLI{outputs: map[string]string{"up --reset": "invalid key: " + key}, errs: map[string]error{"up --reset": errors.New("failure")}}
	s := newTestService(t, cli)
	s.settings = Settings{LoginServer: "https://headscale.example", ShareSubnet: true, Subnet: "192.168.1.0/24"}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !s.Status(context.Background()).Connecting })
	args := cli.called("up --reset")
	if !slices.Contains(args, "--login-server=https://headscale.example") || !slices.Contains(args, "--advertise-routes=192.168.1.0/24") {
		t.Fatal(args)
	}
	if strings.Contains(strings.Join(args, " "), key) || strings.Contains(s.lastError, key) {
		t.Fatal("key exposed")
	}
	for _, arg := range args {
		if file, ok := strings.CutPrefix(arg, "--auth-key=file:"); ok {
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatal("key file retained", err)
			}
		}
	}
	data, _ := os.ReadFile(filepath.Join(s.DataDir, "tailscale.json"))
	if strings.Contains(string(data), key) {
		t.Fatal("key in settings")
	}
	if len(cli.keys) != 1 || cli.keys[0] != key {
		t.Fatal("CLI did not receive pre-authentication key")
	}
}
func TestHeadscaleSubnetChangeKeepsController(t *testing.T) {
	cli := &fakeCLI{outputs: map[string]string{"status --json": runningStatus}}
	s := newTestService(t, cli)
	s.settings.LoginServer = "https://headscale.example"
	if err := s.SaveSettings(context.Background(), Settings{LoginServer: s.settings.LoginServer, ShareSubnet: true, Subnet: "192.168.1.0/24"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(cli.called("up --reset"), "--login-server=https://headscale.example") {
		t.Fatal(cli.calls)
	}
}

func TestStartupRemovesAbandonedLoginKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".tailscale-key-abandoned")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{DataDir: dir, Run: (&fakeCLI{}).run})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("abandoned login credential retained", err)
	}
}
