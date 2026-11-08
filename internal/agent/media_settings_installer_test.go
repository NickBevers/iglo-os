package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMediaSettingsInstaller(t *testing.T) {
	for _, scenario := range []string{"apply", "missing binary", "oversized", "non-regular"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			stack := filepath.Join(dir, "stacks", "arr")
			bin := filepath.Join(dir, "bin")
			if err := os.MkdirAll(stack, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			write := func(path string, data []byte) {
				t.Helper()
				if err := os.WriteFile(path, data, 0700); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"stacks/arr/install.sh", "lib.sh", "storage.sh", "secrets.sh"} {
				data, err := os.ReadFile(filepath.Join("../../deploy", name))
				if err != nil {
					t.Fatal(err)
				}
				if name == "lib.sh" && os.Geteuid() != 0 {
					// The fixture permits a local user to reach the real Proxmox
					// checks. The full installer and all other guards stay intact.
					data = []byte(strings.Replace(string(data), "[[ $EUID -eq 0 ]]", "[[ 0 -eq 0 ]]", 1))
				}
				write(filepath.Join(dir, name), data)
			}
			if scenario != "missing binary" {
				write(filepath.Join(stack, "iglo-arr"), []byte("fixture binary"))
			}
			settings := filepath.Join(dir, "settings.json")
			content := []byte("{\n  \"series\": {\"subtitles\": [\"nl\", \"en\"], \"audio\": \"original\"}\n}\n \t")
			if scenario == "non-regular" {
				if err := os.Mkdir(settings, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				if scenario == "oversized" {
					content = []byte(strings.Repeat(" ", 65537))
				}
				write(settings, content)
			}
			write(filepath.Join(bin, "pveversion"), []byte("#!/bin/sh\nprintf 'pveversion %s\\n' \"$*\" >> \"$CALLS\"\necho pve-manager/9.0.0\n"))
			write(filepath.Join(bin, "dpkg"), []byte("#!/bin/sh\nprintf 'dpkg %s\\n' \"$*\" >> \"$CALLS\"\necho amd64\n"))
			write(filepath.Join(bin, "pct"), []byte(`#!/bin/sh
printf 'pct %s\n' "$*" >> "$CALLS"
case "$1" in
    config) echo 'tags: iglo;media' ;;
    status) echo 'status: running' ;;
    push) exit 0 ;;
    exec) cat > "$STDIN_FILE" ;;
    *) exit 1 ;;
esac
`))
			if runtime.GOOS == "darwin" {
				// The installer uses GNU stat on Proxmox. Match that call on macOS.
				write(filepath.Join(bin, "stat"), []byte("#!/bin/sh\n[ \"$1 $2 $3\" = '-c %s --' ] || exit 1\nwc -c < \"$4\"\n"))
			}
			calls := filepath.Join(dir, "calls")
			stdin := filepath.Join(dir, "stdin")
			cmd := exec.Command("bash", filepath.Join(stack, "install.sh"), "--media-settings", settings, "--ctid", "101")
			cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "CALLS="+calls, "STDIN_FILE="+stdin)
			output, err := cmd.CombinedOutput()
			if scenario != "apply" {
				message := map[string]string{
					"missing binary": "missing iglo-arr next to install.sh",
					"oversized":      "the media settings file exceeds 64 KiB",
					"non-regular":    "the media settings file must be a regular file",
				}[scenario]
				if err == nil || !strings.Contains(string(output), message) {
					t.Fatalf("error = %v, output = %s, want %q", err, output, message)
				}
				commands, _ := os.ReadFile(calls)
				if strings.Contains(string(commands), "pct push ") || strings.Contains(string(commands), "pct exec ") {
					t.Fatalf("rejected input reached container: %s", commands)
				}
				return
			}
			if err != nil {
				t.Fatalf("installer: %v: %s", err, output)
			}
			commands, err := os.ReadFile(calls)
			want := "pveversion \ndpkg --print-architecture\npct config 101\npct status 101\npct push 101 " + filepath.Join(stack, "iglo-arr") + " /usr/local/bin/iglo-arr --perms 0755\npct exec 101 -- /usr/local/bin/iglo-arr media set\n"
			if err != nil || string(commands) != want {
				t.Fatalf("commands = %s, error = %v, want %s", commands, err, want)
			}
			got, err := os.ReadFile(stdin)
			if err != nil || string(got) != string(content) {
				t.Fatalf("stdin = %q, error = %v, want %q", got, err, content)
			}
		})
	}
}
