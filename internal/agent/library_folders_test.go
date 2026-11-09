package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// An empty library folder gets a hidden placeholder, so Jellyfin registers
// the library at its first scan instead of skipping it as empty.
func TestMakeShareFoldersKeepsEmptyLibraries(t *testing.T) {
	storage, err := filepath.Abs("../../deploy/storage.sh")
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, share string, readOnly bool, folders ...string) error {
		t.Helper()
		mode := "false"
		if readOnly {
			mode = "true"
		}
		script := `log() { :; }; die() { echo "$*" >&2; exit 1; }; source "$1"; shift; BINDING_PATH="$1" BINDING_READ_ONLY="$2"; shift 2; make_share_folders "$@"`
		args := append([]string{"-c", script, "test", storage, share, mode}, folders...)
		out, err := exec.Command("bash", args...).CombinedOutput()
		if err != nil {
			t.Logf("bash: %s", out)
		}
		return err
	}
	placeholder := func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, ".iglo-library"))
		return err == nil
	}

	share := t.TempDir()
	if err := os.Mkdir(filepath.Join(share, "series"), 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(share, "anime"), 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(share, "anime", "show.mkv"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(t, share, false, "movies", "series", "anime"); err != nil {
		t.Fatal(err)
	}
	if !placeholder(filepath.Join(share, "movies")) {
		t.Error("a new folder has no placeholder")
	}
	if !placeholder(filepath.Join(share, "series")) {
		t.Error("an existing empty folder has no placeholder")
	}
	if placeholder(filepath.Join(share, "anime")) {
		t.Error("a folder with files got a placeholder")
	}

	// A second run changes nothing: the placeholder itself keeps the folder non-empty.
	before, _ := os.ReadFile(filepath.Join(share, "movies", ".iglo-library"))
	if err := run(t, share, false, "movies"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(share, "movies", ".iglo-library"))
	if string(before) != string(after) || len(after) == 0 {
		t.Errorf("placeholder changed: %q to %q", before, after)
	}

	// A read-only share gets no placeholder in an existing empty folder.
	readOnly := t.TempDir()
	if err := os.Mkdir(filepath.Join(readOnly, "movies"), 0o775); err != nil {
		t.Fatal(err)
	}
	if err := run(t, readOnly, true, "movies"); err != nil {
		t.Fatal(err)
	}
	if placeholder(filepath.Join(readOnly, "movies")) {
		t.Error("a read-only share got a placeholder")
	}
}
