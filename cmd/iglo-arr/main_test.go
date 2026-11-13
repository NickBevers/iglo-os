package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"iglo/internal/stacks/arr"
)

func captureMediaGet(t *testing.T, envPath string) ([]byte, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = previous }()
	err = mediaSettings([]string{"get", "--env", envPath})
	writer.Close()
	output, readErr := io.ReadAll(reader)
	reader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	return output, err
}

func TestMediaGetStdoutIsJSON(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), "stack.vars")
	path, _, _ := arr.MediaSettingsPaths(envPath)
	want := arr.DefaultMediaSettings()
	want.UHDFilms = true
	want.Series.Subtitles = []string{"nl", "en"}
	want.Sizes = arr.SizeLimits{Series: 41, Films: 66, Films4K: 208}
	data, _ := json.Marshal(map[string]any{"version": 1, "settings": want})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := captureMediaGet(t, envPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := arr.DecodeMediaSettings(bytes.NewReader(output))
	if err != nil || len(got.Series.Subtitles) != 2 || got.Series.Subtitles[0] != "nl" || got.Sizes != want.Sizes || !got.UHDFilms {
		t.Fatalf("stdout: %s, error: %v", output, err)
	}
}

func TestMediaSetRejectsInvalidSizesBeforeReadingKeys(t *testing.T) {
	for _, kind := range []string{"series", "films", "films4k"} {
		maximum := 400
		if kind == "films4k" {
			maximum = 1000
		}
		for _, limit := range []int{-1, 9, maximum + 1} {
			settings := arr.DefaultMediaSettings()
			if kind == "series" {
				settings.Sizes.Series = limit
			} else if kind == "films" {
				settings.Sizes.Films = limit
			} else {
				settings.Sizes.Films4K = limit
			}
			data, _ := json.Marshal(settings)
			path := filepath.Join(t.TempDir(), "input.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			before := os.Stdin
			os.Stdin = file
			err = mediaSettings([]string{"set", "--env", filepath.Join(t.TempDir(), "missing.vars")})
			os.Stdin = before
			file.Close()
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("maximum download size must be 0 or between 10 and %d", maximum)) {
				t.Fatalf("%s, limit %d: %v", kind, limit, err)
			}
		}
	}
}

func TestMediaRenderWithoutRunningApps(t *testing.T) {
	root := t.TempDir()
	template := []byte("sonarr:\n  series:\n    quality_definition:\n      type: series\n")
	if err := os.MkdirAll(filepath.Join(root, "config", "recyclarr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "recyclarr.template.yml"), template, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mediaSettings([]string{"render", "--env", filepath.Join(root, "stack.vars")}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "config", "recyclarr", "recyclarr.yml"))
	// Without a size limit, only the fixed Bluray-1080p minimum is added.
	if err != nil || !bytes.HasPrefix(got, template) || !bytes.Contains(got, []byte(`name: "Bluray-1080p"`)) || !bytes.Contains(got, []byte("min: 15")) || bytes.Contains(got, []byte("max:")) {
		t.Fatalf("rendered %s, %v", got, err)
	}
}

func TestMediaGetUnreadable(t *testing.T) {
	output, err := captureMediaGet(t, filepath.Join(t.TempDir(), "stack.vars"))
	if !errors.Is(err, arr.ErrMediaSettingsUnreadable) || err.Error() != "Iglo-OS can't read Bazarr's settings: its settings file is missing or can't be opened. Update the media stack on the Apps page." || len(output) != 0 {
		t.Fatalf("stdout: %s, error: %v", output, err)
	}
}

func TestRecyclarrSyncOutput(t *testing.T) {
	for _, test := range []struct {
		output string
		ok     bool
	}{
		{"[INF] Total of 14 sizes were synced for quality definition series", true},
		{"[INF] All sizes for quality definition movie are already up to date!", true},
		{"", false},
		{"[INF] Found 0 instances to sync", false},
		{"[ERR] Config parsing failed", false},
		{"Configuration loading failed", false},
		{"Failed to process configuration\nAll sizes for quality definition movie are already up to date!", false},
		{"Config parsing failed\nAll sizes for quality definition series are already up to date!", false},
	} {
		if err := validateRecyclarrSyncOutput(test.output); (err == nil) != test.ok {
			t.Errorf("output %q: %v", test.output, err)
		}
	}
}

func TestMediaSetRejectsInvalidFilmSwitchBeforeReadingKeys(t *testing.T) {
	data, _ := json.Marshal(arr.DefaultMediaSettings())
	for _, value := range []string{`"true"`, `1`, `[]`} {
		input := bytes.ReplaceAll(data, []byte(`"uhdFilms":false`), []byte(`"uhdFilms":`+value))
		path := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(path, input, 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		before := os.Stdin
		os.Stdin = file
		err = mediaSettings([]string{"set", "--env", filepath.Join(t.TempDir(), "missing.vars")})
		os.Stdin = before
		file.Close()
		if err == nil || !strings.Contains(err.Error(), "uhdFilms") {
			t.Fatalf("switch %s: %v", value, err)
		}
	}
}
