package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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
	want.Series.Subtitles = []string{"nl", "en"}
	data, _ := json.Marshal(map[string]any{"version": 1, "settings": want})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := captureMediaGet(t, envPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := arr.DecodeMediaSettings(bytes.NewReader(output))
	if err != nil || len(got.Series.Subtitles) != 2 || got.Series.Subtitles[0] != "nl" {
		t.Fatalf("stdout: %s, error: %v", output, err)
	}
}

func TestMediaGetUnreadable(t *testing.T) {
	output, err := captureMediaGet(t, filepath.Join(t.TempDir(), "stack.vars"))
	if !errors.Is(err, arr.ErrMediaSettingsUnreadable) || err.Error() != "Iglo-OS can't read Bazarr's settings: its settings file is missing or can't be opened. Update the media stack on the Apps page." || len(output) != 0 {
		t.Fatalf("stdout: %s, error: %v", output, err)
	}
}
