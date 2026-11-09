package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"iglo/internal/stacks/arr"
)

// ErrMediaSettingsNeedsUpdate means the container has an older iglo-arr.
var ErrMediaSettingsNeedsUpdate = errors.New("the media stack needs Update first to read its media settings")

// ErrMediaSettingsUnavailable means Bazarr has not answered yet.
var ErrMediaSettingsUnavailable = arr.ErrMediaSettingsUnavailable

// ErrMediaSettingsUnreadable means Bazarr data needs a change in its own UI.
var ErrMediaSettingsUnreadable = arr.ErrMediaSettingsUnreadable

type MediaSettingsRequest struct {
	VMID     int               `json:"vmid"`
	Settings arr.MediaSettings `json:"settings"`
}

func (i *AppInstaller) MediaSettings(ctx context.Context, vmid int) (arr.MediaSettings, error) {
	if vmid < 100 || vmid > 999999999 {
		return arr.MediaSettings{}, ErrInvalidContainer
	}

	output, err := i.output(ctx, "pct", "exec", strconv.Itoa(vmid), "--", "/usr/local/bin/iglo-arr", "media", "get")
	if err != nil {
		stderr := string(output)
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			stderr += string(exit.Stderr)
		}
		if strings.Contains(stderr, ErrMediaSettingsUnavailable.Error()) {
			return arr.MediaSettings{}, ErrMediaSettingsUnavailable
		}
		if _, reason, found := strings.Cut(stderr, ErrMediaSettingsUnreadable.Error()); found {
			return arr.MediaSettings{}, fmt.Errorf("%w%s", ErrMediaSettingsUnreadable, strings.SplitN(reason, "\n", 2)[0])
		}
		// Older iglo-arr prints its command list for an unknown command.
		if strings.Contains(stderr, "usage: iglo-arr configure") && !strings.Contains(stderr, "iglo-arr media ") {
			return arr.MediaSettings{}, ErrMediaSettingsNeedsUpdate
		}
		return arr.MediaSettings{}, fmt.Errorf("could not read the media settings of container %d: %w", vmid, err)
	}

	settings, err := arr.DecodeMediaSettings(bytes.NewReader(output))
	if err != nil {
		return arr.MediaSettings{}, fmt.Errorf("could not read the media settings of container %d: %w", vmid, err)
	}
	return settings, nil
}

func (i *AppInstaller) ChangeMediaSettings(vmid int, settings arr.MediaSettings) error {
	if vmid < 100 || vmid > 999999999 {
		return ErrInvalidContainer
	}
	if err := settings.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidAnswers, err)
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	script, err := i.prepare(MediaStackApp)
	if err != nil {
		return err
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	path := filepath.Join(i.Dir, "media.settings")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// A file left by a stopped agent must also have private permissions.
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(data)
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return err
	}

	return i.run(MediaStackApp, ActionMediaSettings, script, []string{"--media-settings", path, "--ctid", strconv.Itoa(vmid)}, path)
}
