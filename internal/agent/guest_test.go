package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGuestComposeRunsInProjectFolder runs guest.sh update against a fake
// docker. The labels list the files in reverse order, like the old script
// left them on Photos. Compose must run in the project folder and must not
// get the label's files.
func TestGuestComposeRunsInProjectFolder(t *testing.T) {
	bin := t.TempDir()
	project := t.TempDir()
	log := filepath.Join(t.TempDir(), "docker.log")
	docker := `#!/bin/sh
if [ "$1" = ps ]; then
	echo 'immich|` + project + `'
	echo 'immich|` + project + `'
	echo '|'
	exit 0
fi
echo "$(pwd) $*" >> "` + log + `"
`
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(docker), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "guest.sh")
	if err := os.WriteFile(script, []byte(guestScript), 0o644); err != nil {
		t.Fatal(err)
	}
	// A fake apt-get keeps the test off the real package manager on CI.
	if err := os.WriteFile(filepath.Join(bin, "apt-get"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script, "update")
	cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")
	cmd.Dir = t.TempDir()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("guest.sh update: %v\n%s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		real + " compose -p immich pull -q --ignore-buildable",
		real + " compose -p immich up -d",
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Fatalf("docker calls:\n%s", data)
	}
	for i, w := range want {
		got := lines[i]
		if dir, rest, ok := strings.Cut(got, " "); ok {
			if resolved, err := filepath.EvalSymlinks(dir); err == nil {
				got = resolved + " " + rest
			}
		}
		if got != w {
			t.Errorf("call %d = %q, want %q", i, got, w)
		}
	}
	if strings.Contains(string(data), " -f ") {
		t.Errorf("compose got files from labels:\n%s", data)
	}
}
