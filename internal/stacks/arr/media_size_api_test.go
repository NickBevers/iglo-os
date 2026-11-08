package arr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func mediaPinnedDefinitions(service string) []map[string]any {
	var names []string
	for name := range pinnedQualitySizes[service] {
		names = append(names, name)
	}
	slices.Sort(names)
	var definitions []map[string]any
	for i, name := range names {
		defaults := pinnedQualitySizes[service][name]
		definitions = append(definitions, map[string]any{
			// Definition IDs belong to the database, not to Quality.cs.
			"id": float64(100 + i), "quality": map[string]any{"id": float64(i), "name": name},
			"title": "Custom " + name, "weight": float64(200 + i), "extra": "keep",
			"minSize": defaults.Min, "maxSize": qualitySizeValue(defaults.Max), "preferredSize": qualitySizeValue(defaults.Preferred),
		})
	}
	return definitions
}

func TestPinnedAppQualityDefaults(t *testing.T) {
	if len(pinnedQualitySizes["sonarr"]) != 22 || len(pinnedQualitySizes["radarr"]) != 30 {
		t.Fatal("pinned quality tables are incomplete")
	}
	for _, test := range []struct {
		service, name      string
		minimum            float64
		maximum, preferred any
	}{
		{"sonarr", "SDTV", 2, float64(100), float64(95)},
		{"sonarr", "Unknown", 1, 199.9, float64(95)},
		{"sonarr", "Raw-HD", 4, nil, float64(95)},
		{"sonarr", "Bluray-1080p Remux", 35, nil, float64(95)},
		{"radarr", "SDTV", 0, float64(100), float64(95)},
		{"radarr", "Bluray-576p", 0, float64(100), float64(95)},
		{"radarr", "BR-DISK", 0, nil, nil},
		{"radarr", "Raw-HD", 0, nil, nil},
		{"radarr", "Bluray-1080p", 0, nil, nil},
	} {
		got, err := defaultQualitySize(test.service, test.name)
		if err != nil || got.Min != test.minimum || qualitySizeValue(got.Max) != test.maximum || qualitySizeValue(got.Preferred) != test.preferred {
			t.Errorf("%s/%s: %+v, %v", test.service, test.name, got, err)
		}
	}
}

func TestMediaUnmanagedQualityLimitsAndReset(t *testing.T) {
	for _, limits := range []SizeLimits{{Series: 125, Films: 100}, {Series: 83, Films: 66}, {Series: 62, Films: 50}, {Series: 41, Films: 33}, {Series: 10, Films: 10}, {Series: 400, Films: 400}} {
		t.Run(fmt.Sprintf("%d/%d", limits.Series, limits.Films), func(t *testing.T) {
			fake := newMediaFake()
			cfg := mediaHandlerConfig(t, fake)
			config, _ := os.ReadFile(cfg.RecyclarrPath)
			before := map[string][]map[string]any{}
			for _, service := range []string{"sonarr", "radarr"} {
				before[service] = mediaPinnedDefinitions(service)
			}
			syncs := 0
			cfg.SyncRecyclarr = func(context.Context) error {
				syncs++
				fake.mu.Lock()
				fake.writes = append(fake.writes, "SYNC")
				fake.mu.Unlock()
				return nil
			}
			settings := DefaultMediaSettings()
			settings.Sizes = limits
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			for _, service := range []struct {
				name      string
				limit     int
				unmanaged int
			}{{"sonarr", limits.Series, 8}, {"radarr", limits.Films, 16}} {
				managed, err := managedQualityNames(config, service.name, "")
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				for i, got := range fake.resources[service.name+"/qualitydefinition"] {
					name := got["quality"].(map[string]any)["name"].(string)
					if managed[strings.ToLower(name)] {
						if !reflect.DeepEqual(got, before[service.name][i]) {
							t.Fatalf("direct API changed managed quality %s/%s", service.name, name)
						}
						continue
					}
					count++
					maximum := got["maxSize"].(float64)
					preferred := got["preferredSize"].(float64)
					minimum := got["minSize"].(float64)
					wantMax, wantPreferred := float64(service.limit), float64(95)
					wantMin := float64(0)
					if name != "Raw-HD" && name != "BR-DISK" {
						defaultMax := float64(100)
						if service.name == "sonarr" && name == "Unknown" {
							defaultMax = 199.9
						}
						wantMax = math.Min(defaultMax, wantMax)
					}
					if service.name == "radarr" && (name == "Raw-HD" || name == "BR-DISK") {
						wantPreferred = wantMax
					}
					wantPreferred = math.Min(wantPreferred, wantMax)
					if service.name == "sonarr" {
						wantMin = 2
						if name == "Unknown" {
							wantMin = 1
						}
						if name == "Raw-HD" {
							wantMin = 4
						}
					}
					wantMin = math.Min(wantMin, wantMax/2)
					if maximum != wantMax || preferred != wantPreferred || minimum != wantMin || minimum >= preferred || preferred > maximum {
						t.Fatalf("%s/%s: got min/preferred/max %.1f/%.1f/%.1f, want %.1f/%.1f/%.1f", service.name, name, minimum, preferred, maximum, wantMin, wantPreferred, wantMax)
					}
					for _, field := range []string{"id", "quality", "title", "weight", "extra"} {
						if !reflect.DeepEqual(got[field], before[service.name][i][field]) {
							t.Fatalf("lost %s on %s/%s", field, service.name, name)
						}
					}
				}
				if count != service.unmanaged {
					t.Fatalf("%s: covered %d unmanaged qualities, want %d", service.name, count, service.unmanaged)
				}
			}
			seenSync := false
			for _, write := range fake.writes {
				if write == "SYNC" {
					seenSync = true
				}
				if strings.Contains(write, "/qualitydefinition/") && !seenSync {
					t.Fatalf("API size update before Recyclarr: %s", write)
				}
			}
			writes := len(fake.writes)
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			if len(fake.writes) != writes || syncs != 1 {
				t.Fatal("identical sizes wrote API values or synced again")
			}
			settings.Sizes = SizeLimits{}
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			for service, defaults := range before {
				if !reflect.DeepEqual(fake.resources[service+"/qualitydefinition"], defaults) {
					t.Fatalf("%s reset did not restore exact defaults, including nulls", service)
				}
			}
			if syncs != 2 {
				t.Fatalf("reset syncs: %d", syncs)
			}
		})
	}
}

func TestMediaSizeAPIFailureRestoresAllSizes(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaHandlerConfig(t, fake)
	beforeYAML, _ := os.ReadFile(cfg.RecyclarrPath)
	before, _ := json.Marshal(map[string]any{"sonarr": fake.resources["sonarr/qualitydefinition"], "radarr": fake.resources["radarr/qualitydefinition"]})
	for _, row := range fake.resources["radarr/qualitydefinition"] {
		if row["quality"].(map[string]any)["name"] == "CAM" {
			fake.fail = fmt.Sprintf("PUT radarr/qualitydefinition/%d", jsonInt(row["id"]))
		}
	}
	cfg.SyncRecyclarr = func(context.Context) error {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		for _, row := range fake.resources["sonarr/qualitydefinition"] {
			if row["quality"].(map[string]any)["name"] == "HDTV-720p" {
				row["maxSize"], row["preferredSize"] = float64(41), float64(41)
			}
		}
		for _, row := range fake.resources["radarr/qualitydefinition"] {
			if row["quality"].(map[string]any)["name"] == "WEBDL-2160p" {
				row["maxSize"], row["preferredSize"] = float64(208), float64(208)
			}
		}
		return nil
	}
	settings := DefaultMediaSettings()
	settings.Sizes = SizeLimits{Series: 41, Films: 33, Films4K: 208}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil || !strings.Contains(err.Error(), "apply unmanaged quality sizes") {
		t.Fatalf("API error: %v", err)
	}
	afterYAML, _ := os.ReadFile(cfg.RecyclarrPath)
	after, _ := json.Marshal(map[string]any{"sonarr": fake.resources["sonarr/qualitydefinition"], "radarr": fake.resources["radarr/qualitydefinition"]})
	if string(beforeYAML) != string(afterYAML) || string(before) != string(after) {
		t.Fatal("API failure did not restore all managed and unmanaged sizes")
	}
	if _, err := os.Stat(cfg.SettingsPath); !os.IsNotExist(err) {
		t.Fatal("API failure saved settings")
	}
}

func TestMediaSizeRollbackDoesNotTrustCachedAPIValues(t *testing.T) {
	fake := newMediaFake()
	before := map[string][]map[string]any{"sonarr": mediaPinnedDefinitions("sonarr"), "radarr": mediaPinnedDefinitions("radarr")}
	cfg := mediaHandlerConfig(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/qualitydefinition") {
			// Model the pinned service cache: Recyclarr's bulk update changed
			// the database, but All still returns the earlier definitions.
			service := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
			json.NewEncoder(w).Encode(before[service])
			return
		}
		fake.ServeHTTP(w, r)
	}))
	cfg.SyncRecyclarr = func(context.Context) error {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		for _, row := range fake.resources["sonarr/qualitydefinition"] {
			if row["quality"].(map[string]any)["name"] == "HDTV-720p" {
				row["maxSize"], row["preferredSize"] = float64(41), float64(41)
			}
		}
		for _, row := range fake.resources["radarr/qualitydefinition"] {
			if row["quality"].(map[string]any)["name"] == "WEBDL-2160p" {
				row["maxSize"], row["preferredSize"] = float64(208), float64(208)
			}
		}
		return errors.New("partial sync failure")
	}
	settings := DefaultMediaSettings()
	settings.Sizes = SizeLimits{Series: 41, Films4K: 208}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil {
		t.Fatal("expected sync failure")
	}
	for service, definitions := range before {
		if !reflect.DeepEqual(fake.resources[service+"/qualitydefinition"], definitions) {
			t.Fatalf("%s rollback trusted stale cached values", service)
		}
	}
}
