package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func validHeadscale() HeadscaleAnswers {
	return HeadscaleAnswers{Hostname: "vpn.example.com", Address: "192.168.1.210/24", Gateway: "192.168.1.1", Storage: "local-lvm", RateMB: 5, Prerequisites: true}
}

func TestHeadscaleAnswersMatchScriptValidation(t *testing.T) {
	cases := map[string]func(*HeadscaleAnswers){"valid": func(*HeadscaleAnswers) {}, "URL": func(a *HeadscaleAnswers) { a.Hostname = "https://vpn.example.com" }, "shell": func(a *HeadscaleAnswers) { a.Hostname = "vpn.example.com\nKEY=value" }, "local": func(a *HeadscaleAnswers) { a.Hostname = "vpn.local" }, "numeric": func(a *HeadscaleAnswers) { a.Hostname = "192.168.1.3" }, "public IP": func(a *HeadscaleAnswers) { a.Address = "8.8.8.8/24"; a.Gateway = "8.8.8.1" }, "network": func(a *HeadscaleAnswers) { a.Address = "192.168.1.0/24" }, "broadcast": func(a *HeadscaleAnswers) { a.Address = "192.168.1.255/24" }, "gateway": func(a *HeadscaleAnswers) { a.Gateway = "192.168.2.1" }, "self gateway": func(a *HeadscaleAnswers) { a.Gateway = "192.168.1.210" }, "storage": func(a *HeadscaleAnswers) { a.Storage = "../root" }, "bandwidth": func(a *HeadscaleAnswers) { a.RateMB = 0 }, "consent": func(a *HeadscaleAnswers) { a.Prerequisites = false }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			a := validHeadscale()
			change(&a)
			err := a.Validate()
			if (err == nil) != (name == "valid") {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "answers")
			data := a.file()
			if !a.Prerequisites {
				data = strings.ReplaceAll(data, "PREREQUISITES=true", "PREREQUISITES=false")
			}
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			out, scriptErr := exec.Command("python3", "../../deploy/stacks/headscale/validate.py", "answers", path).CombinedOutput()
			if (scriptErr == nil) != (err == nil) {
				t.Fatalf("Go/script mismatch: %v / %v %s", err, scriptErr, out)
			}
		})
	}
}

func TestHeadscaleRemovalNeedsMatchingFreshHostAndManagedTags(t *testing.T) {
	for _, test := range []struct {
		name, config, confirm string
		want                  bool
	}{{"match", `tags: iglo;headscale
description: Headscale+controller%0Aiglo-headscale-hostname%3A vpn.example.com`, "vpn.example.com", true}, {"wrong name", `tags: iglo;headscale
description: Headscale+controller%0Aiglo-headscale-hostname%3A vpn.example.com`, "other.example.com", false}, {"wrong app", `tags: iglo;photos
description: iglo-headscale-hostname: vpn.example.com`, "vpn.example.com", false}, {"missing marker", `tags: iglo;headscale`, "vpn.example.com", false}} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			stacks := t.TempDir()
			os.MkdirAll(filepath.Join(stacks, "headscale"), 0700)
			os.WriteFile(filepath.Join(stacks, "headscale", "install.sh"), nil, 0600)
			var args []string
			i := NewAppInstallerWith(dir, stacks, func(_ string, a []string, _, _, _ string) error { args = a; return nil }, func() bool { return false }, func(_ context.Context, name string, a ...string) ([]byte, error) {
				if name != "pct" || strings.Join(a, " ") != "config 210" {
					t.Fatal(name, a)
				}
				return []byte(test.config), nil
			})
			err := i.RemoveHeadscale(context.Background(), 210, test.confirm)
			if (err == nil) != test.want {
				t.Fatal(err)
			}
			if test.want && !strings.Contains(strings.Join(args, " "), "--confirm vpn.example.com") {
				t.Fatal(args)
			}
			if !test.want && len(args) > 0 {
				t.Fatal("started an unconfirmed removal")
			}
			if !errors.Is(i.Remove(HeadscaleApp, 210), ErrInvalidAnswers) {
				t.Fatal("generic removal bypassed confirmation")
			}
		})
	}
}

func TestControllerHostRemovalDecodesProxmoxDescription(t *testing.T) {
	source, err := os.ReadFile("../../deploy/stacks/headscale/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, code, ok := strings.Cut(string(source), "installed_host=$(python3 -c '")
	if !ok {
		t.Fatal("description decoder missing")
	}
	code, _, ok = strings.Cut(code, "' \"$WORK/config\")")
	if !ok {
		t.Fatal("description decoder malformed")
	}
	path := filepath.Join(t.TempDir(), "config")
	os.WriteFile(path, []byte("description: Headscale+controller%0Aiglo-headscale-hostname%3A vpn.example.com\ntags: iglo;headscale\n"), 0600)
	out, err := exec.Command("python3", "-c", code, path).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "vpn.example.com" {
		t.Fatal(string(out), err)
	}
}
