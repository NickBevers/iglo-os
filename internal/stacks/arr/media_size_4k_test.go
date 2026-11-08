package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestMediaSizeOldFileWithoutFilms4K(t *testing.T) {
	settings := DefaultMediaSettings()
	settings.Sizes = SizeLimits{Series: 41, Films: 33}
	data, _ := json.Marshal(settings)
	var old map[string]any
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	delete(old["sizes"].(map[string]any), "films4k")
	data, _ = json.Marshal(map[string]any{"version": 1, "settings": old})
	path := filepath.Join(t.TempDir(), "iglo-media.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := GetMediaSettings(path)
	if err != nil || got.Sizes != settings.Sizes || got.Sizes.Films4K != 0 {
		t.Fatalf("old file: %+v, %v", got.Sizes, err)
	}
	data, _ = json.Marshal(old)
	got, err = DecodeMediaSettings(strings.NewReader(string(data)))
	if err != nil || got.Sizes.Films4K != 0 {
		t.Fatalf("old request: %+v, %v", got.Sizes, err)
	}
}

func TestMediaSizeSplitByQualityName(t *testing.T) {
	sizes := SizeLimits{Series: 41, Films: 33, Films4K: 333}
	for _, test := range []struct {
		service, quality string
		want             int
	}{
		{"sonarr", "WEBDL-1080p", 41}, {"sonarr", "WEBDL-2160p", 41},
		{"radarr", "WEBDL-1080p", 33}, {"radarr", "Bluray-720p", 33}, {"radarr", "BR-DISK", 33},
		{"radarr", "WEBDL-2160p", 333}, {"radarr", "Remux-2160p", 333}, {"radarr", "2160P custom", 333},
	} {
		if got := sizes.forQuality(test.service, test.quality); got != test.want {
			t.Errorf("%s/%s: %d, want %d", test.service, test.quality, got, test.want)
		}
	}
	if got := (SizeLimits{Films: 33}).forQuality("radarr", "Remux-2160p"); got != 0 {
		t.Fatalf("4K no limit inherited HD limit: %d", got)
	}
}

func TestMedia4KGuidePresets(t *testing.T) {
	for _, kind := range []string{"movie", "anime"} {
		guide, err := readQualitySizeGuide("radarr", kind)
		if err != nil {
			t.Fatal(err)
		}
		byName := map[string]guideQualitySize{}
		for _, quality := range guide {
			byName[quality.Name] = quality
		}
		for _, limit := range []int{125, 166, 208, 333, 10, 1000, 0} {
			t.Run(fmt.Sprintf("%s/%d", kind, limit), func(t *testing.T) {
				text, err := qualitySizeOverrides("radarr", kind, SizeLimits{Films: 33, Films4K: limit})
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Qualities []struct {
						Name                string
						Min, Preferred, Max float64
					}
				}
				if err := yaml.Unmarshal([]byte(text), &result); err != nil {
					t.Fatal(err)
				}
				written := map[string]bool{}
				for _, quality := range result.Qualities {
					maximum := 33
					if strings.Contains(quality.Name, "2160p") {
						maximum = limit
					}
					if maximum == 0 || quality.Max != float64(maximum) || quality.Min > float64(maximum)/2 || quality.Min >= quality.Preferred || quality.Preferred > quality.Max {
						t.Fatalf("%s/%s limit %d: %+v", kind, quality.Name, maximum, quality)
					}
					source, ok := byName[quality.Name]
					if !ok || quality.Min != min(source.Min, float64(maximum)/2) || quality.Preferred != min(source.Preferred, float64(maximum)) {
						t.Fatalf("%s/%s did not use the guide and half-limit minimum: %+v", kind, quality.Name, quality)
					}
					written[quality.Name] = true
				}
				for _, quality := range guide {
					maximum := 33
					if strings.Contains(quality.Name, "2160p") {
						maximum = limit
					}
					if want := maximum > 0 && quality.Max > float64(maximum); written[quality.Name] != want {
						t.Errorf("%s override: %v, want %v", quality.Name, written[quality.Name], want)
					}
				}
			})
		}
	}
}

func media4KAPITestConfig(t *testing.T, fake *mediaFake) MediaSettingsConfig {
	t.Helper()
	cfg := mediaHandlerConfig(t, fake)
	// Keep only an HD guide quality so the API must cap every 2160p default.
	guide := filepath.Join(filepath.Dir(cfg.RecyclarrPath), "resources", "docs", "Radarr", "quality-size")
	if err := os.MkdirAll(guide, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"type":"movie","qualities":[{"quality":"WEBRip-1080p","min":12.5,"preferred":1999,"max":2000}]}`)
	if err := os.WriteFile(filepath.Join(guide, "movie.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMedia4KDefaultPresetsAndReset(t *testing.T) {
	for _, limit := range []int{125, 166, 208, 333, 10, 1000} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			fake := newMediaFake()
			cfg := media4KAPITestConfig(t, fake)
			syncs := 0
			cfg.SyncRecyclarr = func(context.Context) error { syncs++; return nil }
			settings := DefaultMediaSettings()
			settings.Sizes = SizeLimits{Series: 41, Films: 33, Films4K: limit}
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, quality := range fake.resources["radarr/qualitydefinition"] {
				name := quality["quality"].(map[string]any)["name"].(string)
				if name == "WEBRip-1080p" {
					continue
				}
				maximum := 33
				if strings.Contains(name, "2160p") {
					maximum = limit
					count++
				}
				minimum, preferred := quality["minSize"].(float64), quality["preferredSize"].(float64)
				if quality["maxSize"] != float64(maximum) || minimum >= preferred || preferred > float64(maximum) {
					t.Fatalf("%s limit %d: %+v", name, maximum, quality)
				}
			}
			if count != 5 {
				t.Fatalf("covered %d 4K defaults, want 5", count)
			}
			writes := len(fake.writes)
			yamlBefore, _ := os.Stat(cfg.RecyclarrPath)
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			yamlAfter, _ := os.Stat(cfg.RecyclarrPath)
			if syncs != 1 || len(fake.writes) != writes || !os.SameFile(yamlBefore, yamlAfter) {
				t.Fatal("unchanged settings wrote API values, YAML or synced again")
			}
			var before []map[string]any
			data, _ := json.Marshal(fake.resources["radarr/qualitydefinition"])
			json.Unmarshal(data, &before)
			settings.Sizes.Films4K = 0
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			for i, quality := range fake.resources["radarr/qualitydefinition"] {
				name := quality["quality"].(map[string]any)["name"].(string)
				if strings.Contains(name, "2160p") {
					if quality["minSize"] != float64(0) || quality["maxSize"] != nil || quality["preferredSize"] != nil {
						t.Fatalf("4K reset lost null defaults: %+v", quality)
					}
				} else if !reflect.DeepEqual(quality, before[i]) {
					t.Fatalf("4K reset changed HD quality %s", name)
				}
			}
			got, err := GetMediaSettings(cfg.SettingsPath)
			if err != nil || got.Sizes != settings.Sizes || syncs != 2 {
				t.Fatalf("saved reset: %+v, syncs %d, error %v", got.Sizes, syncs, err)
			}
		})
	}
}

func TestMedia4KOnlyYAMLRoundTrip(t *testing.T) {
	path := mediaSizesTestConfig(t)
	base, _ := os.ReadFile(path)
	limited, err := mediaSizesYAML(base, SizeLimits{Films4K: 208})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(limited), "max: 208") || strings.Contains(string(limited), `name: "WEBRip-1080p"`) {
		t.Fatalf("4K-only override: %s", limited)
	}
	second, err := mediaSizesYAML(limited, SizeLimits{Films4K: 208})
	if err != nil || string(second) != string(limited) {
		t.Fatalf("4K rewrite is not idempotent: %v", err)
	}
	restored, err := mediaSizesYAML(limited, SizeLimits{})
	if err != nil || string(restored) != string(base) {
		t.Fatalf("no limit did not restore template: %v", err)
	}
}

func TestMedia4KAPIFailureRestoresPreviousLimit(t *testing.T) {
	fake := newMediaFake()
	cfg := media4KAPITestConfig(t, fake)
	cfg.SyncRecyclarr = func(context.Context) error { return nil }
	settings := DefaultMediaSettings()
	settings.Sizes = SizeLimits{Films: 33, Films4K: 125}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	beforeAPI, _ := json.Marshal(fake.resources["radarr/qualitydefinition"])
	beforeYAML, _ := os.ReadFile(cfg.RecyclarrPath)
	beforeSettings, _ := os.ReadFile(cfg.SettingsPath)
	for _, row := range fake.resources["radarr/qualitydefinition"] {
		if row["quality"].(map[string]any)["name"] == "WEBDL-2160p" {
			fake.fail = fmt.Sprintf("PUT radarr/qualitydefinition/%d", jsonInt(row["id"]))
		}
	}
	settings.Sizes.Films4K = 333
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil || !strings.Contains(err.Error(), "apply unmanaged quality sizes") {
		t.Fatalf("expected failed 4K API update: %v", err)
	}
	afterAPI, _ := json.Marshal(fake.resources["radarr/qualitydefinition"])
	afterYAML, _ := os.ReadFile(cfg.RecyclarrPath)
	afterSettings, _ := os.ReadFile(cfg.SettingsPath)
	if string(beforeAPI) != string(afterAPI) || string(beforeYAML) != string(afterYAML) || string(beforeSettings) != string(afterSettings) {
		t.Fatal("failed 4K change did not restore the previous limits and saved settings")
	}
}
