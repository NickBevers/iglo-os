package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestControllerRollbackRestoresIdentitiesAndServices(t *testing.T) {
	data, err := os.ReadFile("../../deploy/stacks/headscale/guest.sh")
	if err != nil {
		t.Fatal(err)
	}
	preamble, _, ok := strings.Cut(string(data), "# Refuse unsupported versions")
	if !ok {
		t.Fatal("bootstrap preamble missing")
	}
	root := t.TempDir()
	backup := filepath.Join(root, "backup")
	files := map[string]string{"bin/headscale": "old binary", "bin/caddy": "old proxy", "config/headscale/config.yaml": "old config", "config/caddy/Caddyfile": "old TLS config", "config/units/headscale.service": "old unit", "config/units/caddy.service": "old proxy unit", "config/nftables.conf": "old firewall", "config/version": "0.29.6", "state/headscale/noise_private.key": "fixture-old-identity", "state/headscale/db.sqlite": "fixture-consistent-database", "state/caddy/certificate.key": "fixture-old-certificate"}
	for path, content := range files {
		full := filepath.Join(backup, path)
		os.MkdirAll(filepath.Dir(full), 0700)
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"usr/local/bin", "etc/headscale", "etc/caddy", "etc/systemd/system", "etc/iglo-headscale", "var/lib/headscale", "var/lib/caddy"} {
		os.MkdirAll(filepath.Join(root, path), 0700)
	}
	os.WriteFile(filepath.Join(root, "var/lib/headscale/noise_private.key"), []byte("fixture-new-identity"), 0600)
	for _, prefix := range []string{"/var/", "/usr/", "/etc/"} {
		preamble = strings.ReplaceAll(preamble, prefix, root+prefix)
	}
	script := preamble + `
BACKUP=$2
BACKUP_COMPLETE=true
STOPPED=true
calls=$3
systemctl(){ printf '%s\n' "$*" >> "$calls"; }
nft(){ :; }
curl(){ return 0; }
false
`
	calls := filepath.Join(root, "calls")
	output, err := exec.Command("bash", "-c", script, "recovery", "update", backup, calls).CombinedOutput()
	if err == nil {
		t.Fatal("failure reported success")
	}
	if !strings.Contains(string(output), "Controller update failed") {
		t.Fatal(string(output))
	}
	for path, want := range map[string]string{"var/lib/headscale/noise_private.key": "fixture-old-identity", "var/lib/headscale/db.sqlite": "fixture-consistent-database", "var/lib/caddy/certificate.key": "fixture-old-certificate", "usr/local/bin/headscale": "old binary", "etc/nftables.conf": "old firewall"} {
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(data) != want {
			t.Fatal(path, string(data), err)
		}
	}
	commands, _ := os.ReadFile(calls)
	if !strings.Contains(string(commands), "start headscale caddy") {
		t.Fatal("old services not restarted", string(commands))
	}
}

func TestControllerPythonBudgetAndBridgeGuards(t *testing.T) {
	dir := t.TempDir()
	resources := filepath.Join(dir, "resources")
	memory := filepath.Join(dir, "memory")
	bridge := filepath.Join(dir, "bridge")
	os.WriteFile(resources, []byte(`[{"type":"qemu","node":"pve","maxmem":30064771072,"status":"stopped"}]`), 0600)
	os.WriteFile(memory, []byte("MemTotal: 33554432 kB\nMemAvailable: 16777216 kB\n"), 0600)
	if out, err := exec.Command("python3", "../../deploy/stacks/headscale/validate.py", "budget", resources, memory, "pve").CombinedOutput(); err == nil {
		t.Fatal("stopped guest exceeded physical budget", string(out))
	}
	os.WriteFile(bridge, []byte(`[{"addr_info":[{"family":"inet","local":"192.168.1.10","prefixlen":24}]}]`), 0600)
	for _, test := range []struct {
		ip string
		ok bool
	}{{"192.168.1.210/24", true}, {"192.168.1.10/24", false}, {"192.168.2.210/24", false}, {"192.168.1.210/16", false}} {
		out, err := exec.Command("python3", "../../deploy/stacks/headscale/validate.py", "network", bridge, test.ip).CombinedOutput()
		if (err == nil) != test.ok {
			t.Fatal(test, err, string(out))
		}
	}
}

func TestControllerStorageRejectsRemoteAndSharedBackends(t *testing.T) {
	for _, test := range []struct {
		data string
		ok   bool
	}{{`{"type":"lvmthin"}`, true}, {`{"type":"dir","path":"/var/lib/vz","shared":0}`, true}, {`{"type":"nfs","path":"/mnt/nas"}`, false}, {`{"type":"cifs"}`, false}, {`{"type":"lvmthin","shared":1}`, false}, {`{"type":"dir"}`, false}} {
		file := filepath.Join(t.TempDir(), "storage.json")
		os.WriteFile(file, []byte(test.data), 0600)
		out, err := exec.Command("python3", "../../deploy/stacks/headscale/validate.py", "storage", file).CombinedOutput()
		if (err == nil) != test.ok {
			t.Fatal(test, err, string(out))
		}
	}
}
