package arr

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Recyclarr v8.8.0 quality-definition schema and TRaSH guide snapshots,
// checked 2026-11-08. Overrides use name, min, max and preferred in MB per minute.
// Every overridden minimum is explicit and at most half the limit. The guide
// clone updates independently of Recyclarr. These snapshots are only a fallback
// when that clone is absent. Names belong to the configured guide type.
//
//go:embed quality_sizes/*.json
var qualitySizeGuides embed.FS

type guideQualitySize struct {
	Name      string  `json:"quality"`
	Min       float64 `json:"min"`
	Max       float64 `json:"max"`
	Preferred float64 `json:"preferred"`
}

// Pinned Radarr v6.4.4.10685 Quality.cs names, checked 2026-11-08, identify
// 4K qualities by 2160p. Use the same split for guide and API definitions.
func (s SizeLimits) forQuality(service, name string) int {
	if service == "radarr" {
		if strings.Contains(strings.ToLower(name), "2160p") {
			return s.Films4K
		}
		return s.Films
	}
	return s.Series
}

func qualitySizeOverrides(service, kind string, sizes SizeLimits, roots ...string) (string, error) {
	if service == "sonarr" && sizes.Series == 0 || service == "radarr" && sizes.Films == 0 && sizes.Films4K == 0 {
		return "", nil
	}
	qualities, err := readQualitySizeGuide(service, kind, roots...)
	if err != nil {
		return "", err
	}
	var result strings.Builder
	for _, quality := range qualities {
		limit := sizes.forQuality(service, quality.Name)
		if limit == 0 || quality.Max <= float64(limit) {
			continue
		}
		if result.Len() == 0 {
			result.WriteString("qualities:\n")
		}
		fmt.Fprintf(&result, "  - name: %s\n    max: %d\n    preferred: %s\n", strconv.Quote(quality.Name), limit,
			strconv.FormatFloat(min(quality.Preferred, float64(limit)), 'f', -1, 64))
		// Explicit min prevents a later guide increase from invalidating max.
		fmt.Fprintf(&result, "    min: %s\n", strconv.FormatFloat(min(quality.Min, float64(limit)/2), 'f', -1, 64))
	}
	return result.String(), nil
}

const sizesBegin = "# Iglo-OS maximum download size begin"
const sizesEnd = "# Iglo-OS maximum download size end"

// mediaSizesYAML uses YAML only to find insertion points. It never serializes
// the rest of the document, so comments, tags and spacing survive each save.
func mediaSizesYAML(data []byte, sizes SizeLimits, roots ...string) ([]byte, error) {
	if err := sizes.Validate(); err != nil {
		return nil, err
	}
	lines := strings.SplitAfter(string(data), "\n")
	base := make([]string, 0, len(lines))
	inside := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case sizesBegin:
			if inside {
				return nil, fmt.Errorf("maximum download size: nested config markers")
			}
			inside = true
		case sizesEnd:
			if !inside {
				return nil, fmt.Errorf("maximum download size: unmatched config marker")
			}
			inside = false
		default:
			if !inside {
				base = append(base, line)
			}
		}
	}
	if inside {
		return nil, fmt.Errorf("maximum download size: unfinished config markers")
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(base, "")), &document); err != nil {
		return nil, fmt.Errorf("read Recyclarr config: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("maximum download size: expected a Recyclarr config mapping")
	}
	type insertion struct {
		line int
		text string
	}
	var inserts []insertion
	for _, service := range []string{"sonarr", "radarr"} {
		if service == "sonarr" && sizes.Series == 0 || service == "radarr" && sizes.Films == 0 && sizes.Films4K == 0 {
			continue
		}
		instances := yamlMappingValue(document.Content[0], service)
		if instances == nil || instances.Kind != yaml.MappingNode || len(instances.Content) == 0 {
			return nil, fmt.Errorf("maximum download size: missing %s instance", service)
		}
		for i := 1; i < len(instances.Content); i += 2 {
			definition := yamlMappingValue(instances.Content[i], "quality_definition")
			kind := yamlMappingValue(definition, "type")
			if kind == nil || kind.Kind != yaml.ScalarNode || definition.Style == yaml.FlowStyle {
				return nil, fmt.Errorf("maximum download size: %s needs a block quality_definition with a type", service)
			}
			if yamlMappingValue(definition, "qualities") != nil {
				return nil, fmt.Errorf("maximum download size: %s already has unmanaged qualities overrides", service)
			}
			overrides, err := qualitySizeOverrides(service, kind.Value, sizes, roots...)
			if err != nil {
				return nil, err
			}
			if overrides == "" {
				continue
			}
			indent := strings.Repeat(" ", definition.Column-1)
			block := indent + sizesBegin + "\n" + indent + strings.ReplaceAll(overrides, "\n", "\n"+indent)
			block += sizesEnd + "\n"
			inserts = append(inserts, insertion{kind.Line, block})
		}
	}
	slices.SortFunc(inserts, func(a, b insertion) int { return b.line - a.line })
	for _, insert := range inserts {
		if !strings.HasSuffix(base[insert.line-1], "\n") {
			return nil, fmt.Errorf("maximum download size: quality definition type needs a line ending")
		}
		base = slices.Insert(base, insert.line, insert.text)
	}
	return []byte(strings.Join(base, "")), nil
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func applyMediaSizes(ctx context.Context, sizes, previous SizeLimits, cfg MediaSettingsConfig) (rollback func() error, err error) {
	rollback = func() error { return nil }
	before, err := os.ReadFile(cfg.RecyclarrPath)
	if err != nil {
		return rollback, fmt.Errorf("read Recyclarr maximum download sizes: %w", err)
	}
	root := filepath.Dir(cfg.RecyclarrPath)
	after, err := mediaSizesYAML(before, sizes, root)
	if err != nil {
		return rollback, err
	}
	apps, err := prepareMediaSizeApps(ctx, cfg, after, root, sizes)
	if err != nil {
		return rollback, err
	}
	changed := !bytes.Equal(before, after)
	needsSync := changed || sizes != previous
	for _, app := range apps {
		needsSync = needsSync || len(app.updates) > 0
	}
	if !needsSync {
		return rollback, nil
	}
	if cfg.SyncRecyclarr == nil {
		return rollback, errors.New("maximum download size: Recyclarr sync is not configured")
	}
	touched := false
	rollback = func() error {
		if !touched {
			return nil
		}
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		var failures []error
		if changed {
			if err := writeFileAtomic(cfg.RecyclarrPath, before, 0o644); err != nil {
				failures = append(failures, fmt.Errorf("restore Recyclarr config: %w", err))
			}
		}
		if err := restoreMediaSizeApps(restoreCtx, apps); err != nil {
			failures = append(failures, fmt.Errorf("restore quality sizes: %w", err))
		}
		return errors.Join(failures...)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, rollback())
		}
	}()
	if changed {
		if err := writeFileAtomic(cfg.RecyclarrPath, after, 0o644); err != nil {
			return rollback, fmt.Errorf("write Recyclarr maximum download sizes: %w", err)
		}
	}
	touched = true
	cfg.Logf("Applying maximum download sizes through Recyclarr")
	if err := cfg.SyncRecyclarr(ctx); err != nil {
		return rollback, fmt.Errorf("maximum download size: Recyclarr sync failed: %w", err)
	}
	// Sync may have updated the local guide clone. Recheck which definitions
	// it now owns before capping the definitions it leaves alone.
	updated, err := prepareMediaSizeApps(ctx, cfg, after, root, sizes)
	if err != nil {
		return rollback, err
	}
	for _, app := range updated {
		for _, definition := range app.updates {
			if err := putQualitySizes(ctx, app.api, definition); err != nil {
				return rollback, fmt.Errorf("apply unmanaged quality sizes: %w", err)
			}
		}
	}
	return rollback, nil
}

func writeMediaSizes(path string, template []byte, sizes SizeLimits) (bool, error) {
	data, err := mediaSizesYAML(template, sizes, filepath.Dir(path))
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if bytes.Equal(existing, data) {
		return false, nil
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return false, fmt.Errorf("write Recyclarr maximum download sizes: %w", err)
	}
	return true, nil
}

// RenderMediaSizes adds stored film profile and size choices to the template
// before the installer starts Recyclarr. It needs no running apps or secrets.
func RenderMediaSizes(templatePath, configPath, settingsPath string, warnings ...func(string, ...any)) error {
	saved, err := readMediaSettings(settingsPath)
	if err != nil {
		warn := func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
		if len(warnings) > 0 {
			warn = warnings[0]
		}
		warn("Warning: could not read media settings: %v. Using the plain Recyclarr template so the stack can update.", err)
		saved.Settings = DefaultMediaSettings()
	}
	template, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}
	template, err = mediaFilmsYAML(template, saved.Settings.UHDFilms)
	if err != nil {
		return err
	}
	_, err = writeMediaSizes(configPath, template, saved.Settings.Sizes)
	return err
}
