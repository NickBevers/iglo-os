package agent

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the host copy step with a release-shaped fixture. A compose file
// present in the bundle must reach the directory used by app installations.
func TestHostInstallsEveryComposeFile(t *testing.T) {
	deploy := filepath.Join("..", "..", "deploy")
	source, err := os.ReadFile(filepath.Join(deploy, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, function, found := strings.Cut(string(source), "install_scripts() {")
	if !found {
		t.Fatal("host install function missing")
	}
	function, _, found = strings.Cut(function, "\n}\n")
	if !found {
		t.Fatal("host install function incomplete")
	}
	scratch := t.TempDir()
	bundle, scripts := filepath.Join(scratch, "bundle"), filepath.Join(scratch, "host")
	err = filepath.WalkDir(deploy, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(deploy, path)
		if err != nil {
			return err
		}
		target := filepath.Join(bundle, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return os.WriteFile(target, []byte(relative), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"VERSION", "stacks/music/iglo-music", "stacks/inbox/iglo-inbox", "stacks/arr/iglo-arr", "stacks/jellyfin/iglo-jellyfin"} {
		if err := os.WriteFile(filepath.Join(bundle, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Redirect host-only destinations into the fixture. The real copy function
	// runs with normal install/cp behavior, without invoking host services.
	script := "set -eu\nBUNDLE_DIR=$1\nSCRIPTS_DIR=$2\n" +
		"systemctl() { :; }\ninstall_power() { :; }\ncopy_stacks() {\n" + function + "\n}\ncopy_stacks\n"
	script = strings.ReplaceAll(script, "/etc/systemd/system/", scripts+"/units/")
	script = strings.ReplaceAll(script, "/usr/local/sbin/", scripts+"/sbin/")
	for _, dir := range []string{"units", "sbin"} {
		if err := os.MkdirAll(filepath.Join(scripts, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := exec.Command("bash", "-c", script, "host-copy", bundle, scripts).CombinedOutput(); err != nil {
		t.Fatalf("host copy: %v: %s", err, output)
	}
	err = filepath.WalkDir(filepath.Join(bundle, "stacks"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "compose") || !strings.HasSuffix(entry.Name(), ".yaml") {
			return nil
		}
		relative, err := filepath.Rel(bundle, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(scripts, relative))
		if err != nil {
			t.Errorf("bundle file %s did not reach host: %v", relative, err)
		} else if string(data) != relative {
			t.Errorf("wrong content for %s", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
