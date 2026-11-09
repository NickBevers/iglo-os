package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"iglo/internal/stacks/arr"
)

func TestMediaSettings(t *testing.T) {
	installer, _ := newTestInstaller(t)
	want := arr.DefaultMediaSettings()
	want.UHDFilms = true
	want.Anime.Subtitles = []string{"nl", "en"}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var command []string
	installer.output = func(_ context.Context, name string, args ...string) ([]byte, error) {
		command = append([]string{name}, args...)
		return data, nil
	}
	got, err := installer.MediaSettings(context.Background(), 130)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("settings = %+v, error = %v", got, err)
	}
	if !slices.Equal(command, []string{"pct", "exec", "130", "--", "/usr/local/bin/iglo-arr", "media", "get"}) {
		t.Fatalf("command = %v", command)
	}
	for _, bad := range []string{"not JSON", "{}", string(data) + " {}", strings.Replace(string(data), `"anime":`, `"unknown":`, 1), strings.Replace(string(data), `"uhdFilms":true`, `"uhdFilms":"yes"`, 1)} {
		data = []byte(bad)
		if _, err := installer.MediaSettings(context.Background(), 130); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	command = nil
	for _, vmid := range []int{1, 99, 1000000000} {
		if _, err := installer.MediaSettings(context.Background(), vmid); !errors.Is(err, ErrInvalidContainer) {
			t.Fatalf("vmid %d: %v", vmid, err)
		}
	}
	if command != nil {
		t.Fatalf("invalid container ran %v", command)
	}
}

func TestMediaSettingsNeedsUpdate(t *testing.T) {
	installer, _ := newTestInstaller(t)
	oldUsage := []byte("usage: iglo-arr configure [--env FILE] [--data-dir DIR] [--bazarr-config FILE] < passwords\n" +
		"       iglo-arr vpn [--env FILE] [--gluetun-dir DIR] < settings\n" +
		"       iglo-arr flaresolverr [--env FILE] on|off\n")
	commandErr := error(&exec.ExitError{Stderr: oldUsage})
	installer.output = func(context.Context, string, ...string) ([]byte, error) {
		return nil, commandErr
	}
	if _, err := installer.MediaSettings(context.Background(), 130); !errors.Is(err, ErrMediaSettingsNeedsUpdate) {
		t.Fatalf("old stack: %v", err)
	}
	for _, failure := range []error{
		&exec.ExitError{Stderr: append(slices.Clone(oldUsage), []byte("       iglo-arr media get|set [--env FILE]\n")...)},
		errors.New("container is stopped"),
	} {
		commandErr = failure
		if _, err := installer.MediaSettings(context.Background(), 130); !errors.Is(err, failure) || errors.Is(err, ErrMediaSettingsNeedsUpdate) {
			t.Fatalf("other failure: %v", err)
		}
	}
}

func TestChangeMediaSettings(t *testing.T) {
	installer, unit := newTestInstaller(t)
	settings := arr.DefaultMediaSettings()
	for _, vmid := range []int{1, 99, 1000000000} {
		if err := installer.ChangeMediaSettings(vmid, settings); !errors.Is(err, ErrInvalidContainer) {
			t.Fatalf("vmid %d: %v", vmid, err)
		}
	}
	invalid := arr.DefaultMediaSettings()
	invalid.Anime.Subtitles = []string{"not-a-language"}
	if err := installer.ChangeMediaSettings(130, invalid); !errors.Is(err, ErrInvalidAnswers) {
		t.Fatalf("invalid settings: %v", err)
	}
	if len(unit.started) != 0 {
		t.Fatalf("invalid input started %v", unit.started)
	}
	path := filepath.Join(installer.Dir, "media.settings")
	if _, err := os.Stat(installer.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid input made files: %v", err)
	}
	if err := os.MkdirAll(installer.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A file from a stopped run must not keep wider permissions.
	if err := os.WriteFile(path, []byte("old settings"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings.UHDFilms = true
	settings.Series.Subtitles = []string{"nl", "en"}
	if err := installer.ChangeMediaSettings(130, settings); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(unit.args, []string{"--media-settings", path, "--ctid", "130"}) ||
		!slices.Equal(unit.started, []string{filepath.Join(installer.StacksDir, "arr", "install.sh"), path}) {
		t.Fatalf("args = %v, started = %v", unit.args, unit.started)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(settings)
	if err != nil || string(data) != string(want) {
		t.Fatalf("settings JSON = %s, want %s, error = %v", data, want, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file info = %v, error = %v", info, err)
	}
	if status := installer.Status(); status.Action != ActionMediaSettings || status.App != MediaStackApp || status.State != UpgradeRunning {
		t.Fatalf("status = %+v", status)
	}
	if err := installer.ChangeMediaSettings(130, arr.DefaultMediaSettings()); !errors.Is(err, ErrAppInstallRunning) {
		t.Fatalf("second change while one runs: %v", err)
	}
	if unchanged, err := os.ReadFile(path); err != nil || string(unchanged) != string(want) {
		t.Fatalf("running settings changed: %s, %v", unchanged, err)
	}
	unit.running = false
	if err := os.WriteFile(installer.exitPath(), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := installer.Status(); status.State != UpgradeFailed || status.Message != "the media settings change stopped with exit code 1" {
		t.Fatalf("failed status = %+v", status)
	}
}

func TestMediaSizeValidationAtAgent(t *testing.T) {
	for _, kind := range []string{"series", "films", "films4k"} {
		for _, limit := range []int{-1, 0, 9, 10, 400, 401, 1000, 1001} {
			installer, unit := newTestInstaller(t)
			settings := arr.DefaultMediaSettings()
			maximum := 400
			if kind == "series" {
				settings.Sizes.Series = limit
			} else if kind == "films" {
				settings.Sizes.Films = limit
			} else {
				settings.Sizes.Films4K = limit
				maximum = 1000
			}
			err := installer.ChangeMediaSettings(130, settings)
			valid := limit == 0 || limit >= 10 && limit <= maximum
			if valid && err != nil || !valid && !errors.Is(err, ErrInvalidAnswers) {
				t.Fatalf("%s, limit %d: %v", kind, limit, err)
			}
			if !valid && len(unit.started) != 0 {
				t.Fatal("invalid limit started an install")
			}
		}
	}
}

func TestChangeMediaSettingsStartFailure(t *testing.T) {
	installer, _ := newTestInstaller(t)
	want := errors.New("could not start the unit")
	installer.start = func(string, []string, string, string, string) error { return want }
	if err := installer.ChangeMediaSettings(130, arr.DefaultMediaSettings()); !errors.Is(err, want) {
		t.Fatalf("start failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installer.Dir, "media.settings")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings file was not removed: %v", err)
	}
}

func TestMediaSettingsUnavailable(t *testing.T) {
	installer, _ := newTestInstaller(t)
	for _, combined := range []bool{false, true} {
		installer.output = func(context.Context, string, ...string) ([]byte, error) {
			message := []byte("error: Bazarr is not answering yet. Try again in a minute.\n")
			if combined {
				return message, &exec.ExitError{}
			}
			return nil, &exec.ExitError{Stderr: message}
		}
		if _, err := installer.MediaSettings(t.Context(), 130); !errors.Is(err, ErrMediaSettingsUnavailable) {
			t.Fatalf("combined %v: %v", combined, err)
		}
	}
}

func TestMediaSettingsUnreadable(t *testing.T) {
	installer, _ := newTestInstaller(t)
	for _, advice := range []string{
		"profile 1 has more than 10 distinct language codes. Fix it in Bazarr under Settings, Languages.",
		"its settings file is missing or can't be opened. Update the media stack on the Apps page.",
		"Bazarr refused the API key. Update the media stack on the Apps page.",
	} {
		want := ErrMediaSettingsUnreadable.Error() + ": " + advice
		for _, combined := range []bool{false, true} {
			installer.output = func(context.Context, string, ...string) ([]byte, error) {
				message := []byte("error: " + want + "\n")
				if combined {
					return message, &exec.ExitError{}
				}
				return nil, &exec.ExitError{Stderr: message}
			}
			_, err := installer.MediaSettings(t.Context(), 130)
			if !errors.Is(err, ErrMediaSettingsUnreadable) || err.Error() != want {
				t.Fatalf("combined %v: %v", combined, err)
			}
		}
	}
}
