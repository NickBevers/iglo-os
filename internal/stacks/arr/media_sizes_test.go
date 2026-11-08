package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func mediaSizesTestConfig(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../../deploy/stacks/arr/recyclarr.yml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "recyclarr.yml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMediaSizeDefaultsAndOldFiles(t *testing.T) {
	if DefaultMediaSettings().Sizes != (SizeLimits{}) {
		t.Fatal("defaults have a size limit")
	}
	path := filepath.Join(t.TempDir(), "iglo-media.json")
	data, _ := json.Marshal(DefaultMediaSettings())
	var old map[string]any
	json.Unmarshal(data, &old)
	delete(old, "sizes")
	data, _ = json.Marshal(map[string]any{"version": 1, "settings": old})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := GetMediaSettings(path)
	if err != nil || got.Sizes != (SizeLimits{}) {
		t.Fatalf("old file: %+v, %v", got, err)
	}
	data, _ = json.Marshal(old)
	got, err = DecodeMediaSettings(bytes.NewReader(data))
	if err != nil || got.Sizes != (SizeLimits{}) {
		t.Fatalf("old request: %+v, %v", got, err)
	}
}

func TestMediaSizeBounds(t *testing.T) {
	for _, kind := range []string{"series", "films"} {
		for _, limit := range []int{-1, 0, 1, 9, 10, 41, 400, 401} {
			t.Run(fmt.Sprintf("%s/%d", kind, limit), func(t *testing.T) {
				settings := DefaultMediaSettings()
				if kind == "series" {
					settings.Sizes.Series = limit
				} else {
					settings.Sizes.Films = limit
				}
				data, _ := json.Marshal(settings)
				_, err := DecodeMediaSettings(bytes.NewReader(data))
				valid := limit == 0 || limit >= 10 && limit <= 400
				if (err == nil) != valid {
					t.Fatalf("limit %d: %v", limit, err)
				}
			})
		}
	}
}

func TestQualitySizeOverrides(t *testing.T) {
	for _, guideName := range []struct{ service, kind string }{{"sonarr", "series"}, {"sonarr", "anime"}, {"radarr", "movie"}, {"radarr", "anime"}} {
		data, err := qualitySizeGuides.ReadFile("quality_sizes/trash-" + guideName.service + "-quality-size-" + guideName.kind + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var guide struct{ Qualities []guideQualitySize }
		if err := json.Unmarshal(data, &guide); err != nil {
			t.Fatal(err)
		}
		// Include every dialog preset for both guides, plus the validation
		// bounds, an unchanged minimum at half the limit and guide maxima.
		for _, limit := range []int{0, 10, 20, 33, 41, 50, 62, 66, 83, 100, 125, 400, 1000, 2000} {
			t.Run(fmt.Sprintf("%s/%s/%d", guideName.service, guideName.kind, limit), func(t *testing.T) {
				text, err := qualitySizeOverrides(guideName.service, guideName.kind, limit)
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Qualities []struct {
						Name           string
						Max, Preferred float64
						Min            *float64
					}
				}
				if err := yaml.Unmarshal([]byte(text), &result); err != nil {
					t.Fatal(err)
				}
				i := 0
				for _, quality := range guide.Qualities {
					if limit == 0 || quality.Max <= float64(limit) {
						continue
					}
					if i >= len(result.Qualities) {
						t.Fatalf("missing %s", quality.Name)
					}
					got := result.Qualities[i]
					if got.Name != quality.Name || got.Max != float64(limit) || got.Preferred != min(quality.Preferred, float64(limit)) {
						t.Fatalf("override %+v for %+v", got, quality)
					}
					minimum := min(quality.Min, float64(limit)/2)
					if got.Min == nil || *got.Min != minimum {
						t.Fatalf("%s minimum: got %v, want %.1f", quality.Name, got.Min, minimum)
					}
					if minimum >= got.Preferred || got.Preferred > got.Max {
						t.Fatalf("%s: want min < preferred <= max, got %.1f < %.1f <= %.1f", quality.Name, minimum, got.Preferred, got.Max)
					}
					i++
				}
				if len(result.Qualities) != i || (i == 0 && strings.Contains(text, "qualities:")) {
					t.Fatalf("unwanted overrides: %s", text)
				}
			})
		}
	}
}

func TestMediaSizesYAMLRoundTrip(t *testing.T) {
	path := mediaSizesTestConfig(t)
	base, _ := os.ReadFile(path)
	base = bytes.ReplaceAll(base, []byte("type: series"), []byte("type: series # keep this inline comment"))
	base = append(base, []byte("\n# Keep this final comment.\n")...)
	for _, sizes := range []SizeLimits{{}, {Series: 41}, {Films: 66}, {Series: 41, Films: 33}} {
		got, err := mediaSizesYAML(base, sizes)
		if err != nil {
			t.Fatal(err)
		}
		var parsed yaml.Node
		if err := yaml.Unmarshal(got, &parsed); err != nil {
			t.Fatalf("bad generated YAML: %v\n%s", err, got)
		}
		second, err := mediaSizesYAML(got, sizes)
		if err != nil || !bytes.Equal(second, got) {
			t.Fatalf("not idempotent: %v", err)
		}
		restored, err := mediaSizesYAML(got, SizeLimits{})
		if err != nil || !bytes.Equal(restored, base) {
			t.Fatalf("lost original bytes: %v\n%s", err, restored)
		}
		if sizes == (SizeLimits{}) && strings.Contains(string(got), "qualities:") {
			t.Fatal("no limit wrote qualities")
		}
	}
}

func TestApplyMediaSizesSyncAndNoWrites(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaHandlerConfig(t, fake)
	base, _ := os.ReadFile(cfg.RecyclarrPath)
	settings := DefaultMediaSettings()
	settings.Sizes = SizeLimits{Series: 41, Films: 66}
	syncs := 0
	cfg.SyncRecyclarr = func(context.Context) error {
		syncs++
		data, _ := os.ReadFile(cfg.RecyclarrPath)
		if !bytes.Contains(data, []byte("max: 41")) || !bytes.Contains(data, []byte("max: 66")) {
			t.Fatal("sync ran before config was written")
		}
		return nil
	}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	yamlBefore, _ := os.Stat(cfg.RecyclarrPath)
	stateBefore, _ := os.Stat(cfg.SettingsPath)
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	yamlAfter, _ := os.Stat(cfg.RecyclarrPath)
	stateAfter, _ := os.Stat(cfg.SettingsPath)
	if !os.SameFile(yamlBefore, yamlAfter) || !os.SameFile(stateBefore, stateAfter) || syncs != 1 {
		t.Fatal("second Apply wrote files or synced again")
	}
	got, err := GetMediaSettings(cfg.SettingsPath)
	if err != nil || got.Sizes != settings.Sizes {
		t.Fatalf("get sizes: %+v, %v", got.Sizes, err)
	}
	cfg.SyncRecyclarr = func(context.Context) error { syncs++; return nil }
	settings.Sizes = SizeLimits{}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(cfg.RecyclarrPath)
	if !bytes.Equal(base, restored) || syncs != 2 {
		t.Fatal("no limit did not restore and sync the original config")
	}
}

func TestMediaSizesSyncFailureRetries(t *testing.T) {
	for _, alreadySaved := range []bool{false, true} {
		t.Run(fmt.Sprint(alreadySaved), func(t *testing.T) {
			cfg := mediaHandlerConfig(t, newMediaFake())
			before, _ := os.ReadFile(cfg.RecyclarrPath)
			settings := DefaultMediaSettings()
			settings.Sizes.Series = 41
			if alreadySaved {
				data, _ := json.Marshal(savedMediaSettings{Version: 1, Settings: settings})
				if err := os.WriteFile(cfg.SettingsPath, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			syncs := 0
			cfg.SyncRecyclarr = func(context.Context) error { syncs++; return errors.New("test sync failure") }
			for range 2 {
				if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil || !strings.Contains(err.Error(), "Recyclarr sync failed: test sync failure") {
					t.Fatalf("sync error: %v", err)
				}
				if _, err := os.Stat(cfg.SettingsPath); !alreadySaved && !os.IsNotExist(err) {
					t.Fatal("failed sync saved settings")
				}
				after, _ := os.ReadFile(cfg.RecyclarrPath)
				if !bytes.Equal(before, after) {
					t.Fatal("failed sync left config changed")
				}
			}
			if syncs != 2 {
				t.Fatal("failed sync was not retried")
			}
		})
	}
}

func TestRenderMediaSizesKeepsSettingsOnUpdate(t *testing.T) {
	path := mediaSizesTestConfig(t)
	template, _ := os.ReadFile(path)
	template = append(template, []byte("# New release comment.\n")...)
	templatePath := filepath.Join(t.TempDir(), "template.yml")
	os.WriteFile(templatePath, template, 0o644)
	settingsPath := filepath.Join(t.TempDir(), "iglo-media.json")
	for _, sizes := range []SizeLimits{{Series: 41, Films: 100}, {}} {
		settings := DefaultMediaSettings()
		settings.Sizes = sizes
		data, _ := json.Marshal(savedMediaSettings{Version: 1, Settings: settings})
		os.WriteFile(settingsPath, data, 0o644)
		if err := RenderMediaSizes(templatePath, path, settingsPath); err != nil {
			t.Fatal(err)
		}
		want, _ := mediaSizesYAML(template, sizes)
		got, _ := os.ReadFile(path)
		if !bytes.Equal(want, got) {
			t.Fatal("update lost settings or new template content")
		}
		before, _ := os.Stat(path)
		if err := RenderMediaSizes(templatePath, path, settingsPath); err != nil {
			t.Fatal(err)
		}
		after, _ := os.Stat(path)
		if !os.SameFile(before, after) {
			t.Fatal("unchanged render wrote config")
		}
	}
}

func TestRenderMediaSizesUnreadableSettings(t *testing.T) {
	path := mediaSizesTestConfig(t)
	template, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	templatePath := filepath.Join(t.TempDir(), "template.yml")
	if err := os.WriteFile(templatePath, template, 0o644); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "iglo-media.json")
	if err := os.WriteFile(settingsPath, []byte("broken settings"), 0o644); err != nil {
		t.Fatal(err)
	}
	limited, err := mediaSizesYAML(template, SizeLimits{Series: 41})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, limited, 0o644); err != nil {
		t.Fatal(err)
	}
	var warning string
	if err := RenderMediaSizes(templatePath, path, settingsPath, func(format string, args ...any) { warning = fmt.Sprintf(format, args...) }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, template) || !strings.Contains(warning, "Warning:") || !strings.Contains(warning, "plain Recyclarr template") {
		t.Fatalf("fallback: %s, warning %q, error %v", got, warning, err)
	}
}

func TestMediaSizesUsesLocalGuide(t *testing.T) {
	root := t.TempDir()
	guideDir := filepath.Join(root, "cache", "resources", "clone-hash", "docs", "Sonarr", "quality-size")
	if err := os.MkdirAll(guideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"type":"series","qualities":[{"quality":"New local quality","min":12.3,"preferred":200,"max":300}]}`)
	if err := os.WriteFile(filepath.Join(guideDir, "series.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := qualitySizeOverrides("sonarr", "series", 41, root)
	if err != nil || !strings.Contains(got, "New local quality") || !strings.Contains(got, "min: 12.3") || strings.Contains(got, "HDTV-720p") {
		t.Fatalf("local guide: %s, %v", got, err)
	}
	// A later guide minimum must not affect the explicit min in saved YAML.
	if !strings.Contains(got, "min: 12.3") {
		t.Fatal("minimum was left to the moving guide")
	}
	fallback, err := qualitySizeOverrides("radarr", "movie", 33, root)
	if err != nil || !strings.Contains(fallback, "HDTV-720p") {
		t.Fatalf("fallback: %s, %v", fallback, err)
	}
}

func TestMediaSizesDoesNotApplyBeforeLanguageFailure(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaHandlerConfig(t, fake)
	before, _ := os.ReadFile(cfg.RecyclarrPath)
	fake.fail = "POST bazarr/system/settings"
	syncs := 0
	cfg.SyncRecyclarr = func(context.Context) error { syncs++; return nil }
	settings := DefaultMediaSettings()
	settings.Sizes.Series = 41
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil {
		t.Fatal("expected language failure")
	}
	after, _ := os.ReadFile(cfg.RecyclarrPath)
	if syncs != 0 || !bytes.Equal(before, after) {
		t.Fatal("sizes applied before the language step succeeded")
	}
}

func TestMediaSizesRestoresOnSettingsSaveFailure(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaHandlerConfig(t, fake)
	fake.resources["sonarr/qualitydefinition"] = []map[string]any{{"id": 1, "quality": map[string]any{"name": "HDTV-720p"}, "minSize": float64(10), "preferredSize": float64(995), "maxSize": float64(1000)}}
	before, _ := os.ReadFile(cfg.RecyclarrPath)
	settings := DefaultMediaSettings()
	oldState, _ := json.Marshal(savedMediaSettings{Version: 1, Settings: settings})
	if err := os.WriteFile(cfg.SettingsPath, oldState, 0o644); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(cfg.SettingsPath)
	backup := parent + ".before-size-test"
	t.Cleanup(func() { os.Remove(parent); os.Rename(backup, parent) })
	cfg.SyncRecyclarr = func(context.Context) error {
		// Simulate Recyclarr updating a managed definition, then make the
		// state folder unwritable without depending on the test user's UID.
		fake.mu.Lock()
		fake.resources["sonarr/qualitydefinition"][0]["maxSize"] = float64(41)
		fake.resources["sonarr/qualitydefinition"][0]["preferredSize"] = float64(41)
		fake.mu.Unlock()
		if err := os.Rename(parent, backup); err != nil {
			return err
		}
		return os.WriteFile(parent, []byte("blocks state writes"), 0o644)
	}
	settings.Sizes.Series = 41
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil || !strings.Contains(err.Error(), "save media settings") {
		t.Fatalf("save failure: %v", err)
	}
	after, _ := os.ReadFile(cfg.RecyclarrPath)
	if !bytes.Equal(before, after) {
		t.Fatal("save failure left YAML changed")
	}
	definition := fake.resources["sonarr/qualitydefinition"][0]
	if definition["maxSize"] != float64(1000) || definition["preferredSize"] != float64(995) {
		t.Fatalf("save failure left API sizes changed: %v", definition)
	}
	saved, _ := os.ReadFile(filepath.Join(backup, filepath.Base(cfg.SettingsPath)))
	if !bytes.Equal(saved, oldState) {
		t.Fatal("save failure changed saved settings")
	}
}

func TestMediaSizesRefusesUnverifiedAppDefaults(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaHandlerConfig(t, fake)
	// A live value may already be capped. It is not evidence of a default.
	fake.resources["sonarr/qualitydefinition"] = []map[string]any{{"id": 1, "quality": map[string]any{"name": "New unverified quality"}, "minSize": float64(0), "preferredSize": float64(41), "maxSize": float64(41)}}
	before, _ := os.ReadFile(cfg.RecyclarrPath)
	syncs := 0
	cfg.SyncRecyclarr = func(context.Context) error { syncs++; return nil }
	settings := DefaultMediaSettings()
	settings.Sizes.Series = 41
	err := ApplyMediaSettings(t.Context(), settings, cfg)
	if err == nil || !strings.Contains(err.Error(), `sonarr quality "New unverified quality" is absent from the pinned app defaults`) {
		t.Fatalf("unverified defaults: %v", err)
	}
	after, _ := os.ReadFile(cfg.RecyclarrPath)
	if syncs != 0 || !bytes.Equal(before, after) || fake.resources["sonarr/qualitydefinition"][0]["maxSize"] != float64(41) {
		t.Fatal("unverified defaults changed sizes")
	}
	if _, err := os.Stat(cfg.SettingsPath); !os.IsNotExist(err) {
		t.Fatal("unverified defaults saved settings")
	}
}
