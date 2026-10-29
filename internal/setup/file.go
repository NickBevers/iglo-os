package setup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"iglo/internal/agent"
	"iglo/internal/vpn"
)

// A plan file is the plan of a setup without its secrets, as YAML. Keep it
// with your own notes. To set up a server again, load it on the Setup page
// and type the passwords and keys.

const (
	// FileVersion is the version of the plan file.
	FileVersion = 1
	// MaxFileSize is the largest plan file that the Setup page takes.
	MaxFileSize = 64 << 10
	// planFile is the plan of the last run, in the data folder of the manager.
	planFile = "setup-plan.yaml"
)

// File is a plan file.
type File struct {
	Version int `yaml:"version"`
	Plan    `yaml:",inline"`
}

// withoutSecrets is the plan with its passwords and keys left out. It copies
// the shares, so the plan of a running setup stays as it is.
func (p Plan) withoutSecrets() Plan {
	plan := p
	plan.Admin.Password, plan.NAS.Account.Password, plan.BackupPassword = "", "", ""
	plan.VPN.WireGuardKey, plan.VPN.PIAUsername, plan.VPN.PIAPassword, plan.VPN.PIADedicatedToken = "", "", "", ""
	// Plan files from before PIA call the locations countries.
	if plan.VPN.Locations == "" {
		plan.VPN.Locations = plan.VPN.Countries
	}
	plan.VPN.Countries = ""
	plan.ConfirmPrivileged = false
	plan.Apps = slices.Clone(p.Apps)
	plan.NAS.Shares = maps.Clone(p.NAS.Shares)
	for role, share := range plan.NAS.Shares {
		share.Password = ""
		plan.NAS.Shares[role] = share
	}
	return plan
}

// MarshalFile writes a plan file, with a header that says what it is.
func MarshalFile(plan Plan) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString("# Iglo-OS setup plan. Load it on the Setup page to set up a server again.\n")
	buffer.WriteString("# Passwords and keys are not in this file. The Setup page asks for them.\n")
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(File{Version: FileVersion, Plan: plan.withoutSecrets()}); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// ParseFile reads a plan file strictly: an unknown key is an error, so a typo
// is not ignored. It checks the shape; the full check comes when the setup
// starts, with the passwords.
func ParseFile(data []byte) (Plan, error) {
	if len(data) > MaxFileSize {
		return Plan{}, fmt.Errorf("%w: the plan file is larger than %d KB", ErrInvalid, MaxFileSize>>10)
	}
	var file File
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		if errors.Is(err, io.EOF) {
			return Plan{}, fmt.Errorf("%w: the plan file is empty", ErrInvalid)
		}
		return Plan{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if file.Version != FileVersion {
		return Plan{}, fmt.Errorf("%w: the plan file must have version %d", ErrInvalid, FileVersion)
	}
	for _, app := range file.Apps {
		if !slices.Contains(Apps, app) {
			return Plan{}, fmt.Errorf("%w: unknown app %q", ErrInvalid, app)
		}
	}
	for role := range file.NAS.Shares {
		if _, known := shareNames[role]; !known {
			return Plan{}, fmt.Errorf("%w: unknown share %q", ErrInvalid, role)
		}
	}
	// Secrets never come from a file, even when someone wrote them in.
	return file.Plan.withoutSecrets(), nil
}

// savePlan keeps the plan of a run without its secrets, for the export.
func (s *Service) savePlan(plan Plan) error {
	if s.DataDir == "" {
		return nil
	}
	data, err := MarshalFile(plan)
	if err != nil {
		return err
	}
	path := filepath.Join(s.DataDir, planFile)
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// ExportPlan returns the plan file of this server: the plan of the last setup,
// or one made from the apps and shares that are there.
func (s *Service) ExportPlan(ctx context.Context) ([]byte, error) {
	if s.DataDir != "" {
		if data, err := os.ReadFile(filepath.Join(s.DataDir, planFile)); err == nil {
			return data, nil
		}
	}
	return MarshalFile(s.currentPlan(ctx))
}

// currentPlan describes the apps and shares of a server that was set up
// without the setup. The accounts on the NAS aren't known, so they stay empty.
func (s *Service) currentPlan(ctx context.Context) Plan {
	plan := Plan{Apps: []string{}, NAS: NAS{Shares: map[string]Share{}}, VPN: VPN{Provider: vpn.ProtonVPN, Locations: "Netherlands"},
		AnimeIndexers: true}
	status, _ := s.Apps.Status(ctx)
	plan.Storage = status.Defaults.Storage
	for _, app := range status.Apps {
		if app.Installed && slices.Contains(Apps, app.ID) {
			plan.Apps = append(plan.Apps, app.ID)
		}
		// FlareSolverr is off unless the media stack runs it.
		if app.ID == agent.MediaStackApp && app.FlareSolverr != nil {
			plan.FlareSolverr = *app.FlareSolverr
		}
	}
	view, err := s.Agent.StorageBindings(ctx)
	if err != nil {
		return plan
	}
	for _, binding := range view.Bindings {
		server, share, ok := splitSource(binding.Source)
		if _, known := shareNames[binding.Role]; !ok || !known {
			continue
		}
		if _, taken := plan.NAS.Shares[binding.Role]; taken {
			continue
		}
		plan.NAS.Server = server
		plan.NAS.Shares[binding.Role] = Share{Share: share}
		plan.ProxmoxBackups = plan.ProxmoxBackups || binding.ProxmoxStorage == agent.ProxmoxBackupStorage(binding.ID)
	}
	return plan
}

// splitSource turns //192.168.1.20/Videos into its server and share.
func splitSource(source string) (string, string, bool) {
	rest, ok := strings.CutPrefix(source, "//")
	if !ok {
		return "", "", false
	}
	server, share, ok := strings.Cut(rest, "/")
	return server, share, ok && server != "" && share != ""
}
