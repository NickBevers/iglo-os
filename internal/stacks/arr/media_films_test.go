package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func filmSnapshot(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "films4k", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFilmSettingsOldFilesAndValidation(t *testing.T) {
	settings := DefaultMediaSettings()
	if settings.UHDFilms {
		t.Fatal("4K enabled by default")
	}
	data, _ := json.Marshal(settings)
	old := bytes.ReplaceAll(data, []byte(`,"uhdFilms":false`), nil)
	path := filepath.Join(t.TempDir(), "iglo-media.json")
	if err := os.WriteFile(path, append(append([]byte(`{"version":1,"settings":`), old...), '}'), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := GetMediaSettings(path)
	if err != nil || got.UHDFilms {
		t.Fatalf("old file: %+v, %v", got, err)
	}
	for _, value := range []string{`"true"`, `1`, `{}`} {
		bad := bytes.ReplaceAll(data, []byte(`"uhdFilms":false`), []byte(`"uhdFilms":`+value))
		if _, err := DecodeMediaSettings(bytes.NewReader(bad)); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	settings.UHDFilms = true
	data, _ = json.Marshal(settings)
	got, err = DecodeMediaSettings(bytes.NewReader(data))
	if err != nil || !got.UHDFilms {
		t.Fatalf("4K request: %+v, %v", got, err)
	}
}

func TestFilmRecyclarrConfig(t *testing.T) {
	base, err := os.ReadFile("../../../deploy/stacks/arr/recyclarr.yml")
	if err != nil {
		t.Fatal(err)
	}
	off, err := mediaFilmsYAML(base, false)
	if err != nil || !bytes.Equal(base, off) {
		t.Fatalf("off changed template: %v", err)
	}
	on, err := mediaFilmsYAML(base, true)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(on, &doc); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, on)
	}
	movies := doc["radarr"].(map[string]any)["movies"].(map[string]any)
	profiles := movies["quality_profiles"].([]any)
	profile := profiles[0].(map[string]any)
	guide := filmSnapshot(t, "trash-quality-profiles-uhd-bluray-web")
	if len(profiles) != 3 || profile["trash_id"] != guide["trash_id"] || profile["name"] != films4KProfile || profile["min_format_score"] != 0 || profile["quality_sort"] != "top" {
		t.Fatalf("profile: %v", profiles)
	}
	if !sameJSON(profile["upgrade"], map[string]any{"allowed": guide["upgradeAllowed"], "until_quality": guide["cutoff"], "until_score": guide["cutoffFormatScore"]}) {
		t.Fatalf("upgrade: %v", profile["upgrade"])
	}
	want := []any{
		map[string]any{"name": "Bluray-2160p"},
		map[string]any{"name": "WEB 2160p", "qualities": []string{"WEBDL-2160p", "WEBRip-2160p"}},
		map[string]any{"name": "Bluray-1080p"},
		map[string]any{"name": "WEB 1080p", "qualities": []string{"WEBDL-1080p", "WEBRip-1080p"}},
	}
	if !sameJSON(profile["qualities"], want) {
		t.Fatalf("qualities: %v", profile["qualities"])
	}
	// Guard every emitted profile key against the pinned schema.
	schema := filmSnapshot(t, "recyclarr-v8.8.0-quality-profiles")["items"].(map[string]any)["properties"].(map[string]any)
	for key := range profile {
		if _, ok := schema[key]; !ok {
			t.Fatalf("unsupported profile key %s", key)
		}
	}
	unwanted := filmSnapshot(t, "trash-cf-groups-unwanted-formats")
	if unwanted["default"] != "true" || unwanted["quality_profiles"].(map[string]any)["include"].(map[string]any)["UHD Bluray + WEB"] != guide["trash_id"] {
		t.Fatal("UHD no longer inherits unwanted formats")
	}
	formats := movies["custom_formats"].([]any)
	cfSchema := filmSnapshot(t, "recyclarr-v8.8.0-custom-formats")["items"].(map[string]any)["properties"].(map[string]any)
	for _, raw := range formats {
		entry := raw.(map[string]any)
		for key := range entry {
			if _, ok := cfSchema[key]; !ok {
				t.Fatalf("unsupported CF key %s", key)
			}
		}
		for _, raw := range entry["assign_scores_to"].([]any) {
			if raw.(map[string]any)["name"] != films4KProfile {
				t.Fatalf("scores leak into another profile: %v", raw)
			}
		}
	}
	for i, name := range []string{"trash-cf-hdr", "trash-cf-hdr10plus-boost"} {
		cf := filmSnapshot(t, name)
		if formats[0].(map[string]any)["trash_ids"].([]any)[i] != cf["trash_id"] || jsonInt(cf["trash_scores"].(map[string]any)["default"]) != []int{500, 100}[i] {
			t.Fatalf("HDR snapshot mismatch: %v", cf)
		}
	}
	dv := filmSnapshot(t, "trash-cf-dv-wo-hdr-fallback")
	entry := formats[1].(map[string]any)
	if entry["trash_ids"].([]any)[0] != dv["trash_id"] || entry["assign_scores_to"].([]any)[0].(map[string]any)["score"] != jsonInt(dv["trash_scores"].(map[string]any)["default"]) {
		t.Fatal("lost DV rejection")
	}
	t.Run("1080p fallback codec scores", func(t *testing.T) {
		hd := filmSnapshot(t, "trash-quality-profiles-hd-bluray-web")
		hdGroup := filmSnapshot(t, "trash-cf-groups-optional-golden-rule-hd")
		uhdGroup := filmSnapshot(t, "trash-cf-groups-optional-golden-rule-uhd")
		hdFormat := hdGroup["custom_formats"].([]any)[0].(map[string]any)
		uhdFormat := uhdGroup["custom_formats"].([]any)[1].(map[string]any)
		if hdGroup["default"] != "true" || hdFormat["default"] != true || hdFormat["name"] != "x265 (HD)" || hdGroup["quality_profiles"].(map[string]any)["include"].(map[string]any)["HD Bluray + WEB"] != hd["trash_id"] {
			t.Fatal("HD guide no longer selects x265 (HD)")
		}
		if uhdGroup["default"] != "true" || uhdFormat["default"] != true || uhdFormat["name"] != "x265 (no HDR/DV)" || uhdGroup["quality_profiles"].(map[string]any)["include"].(map[string]any)["UHD Bluray + WEB"] != guide["trash_id"] {
			t.Fatal("UHD guide no longer selects x265 (no HDR/DV)")
		}
		entry := formats[2].(map[string]any)
		if !sameJSON(entry["trash_ids"], []any{hdFormat["trash_id"]}) || entry["assign_scores_to"].([]any)[0].(map[string]any)["score"] != -10000 {
			t.Fatal("1080p x265 lost the live HD profile's -10000 penalty")
		}
		for _, raw := range formats {
			if slices.Contains(raw.(map[string]any)["trash_ids"].([]any), uhdFormat["trash_id"]) {
				t.Fatal("x265 (no HDR/DV) must inherit its UHD guide score without an override")
			}
		}
		// Iglo-OS has no Radarr release matcher. Check the rendered scores
		// rather than approximating its codec and format matching here.
		if profile["min_format_score"] != 0 || jsonInt(guide["minFormatScore"]) != 0 || jsonInt(hd["minFormatScore"]) != 0 {
			t.Fatal("clean 1080p H.264 with no matching formats must pass at 0")
		}
	})
	for _, name := range []string{"trash-cf-sdr", "trash-cf-sdr-no-webdl"} {
		spec := filmSnapshot(t, name)["specifications"].([]any)[0].(map[string]any)
		if spec["implementation"] != "ResolutionSpecification" || spec["required"] != true || jsonInt(spec["fields"].(map[string]any)["value"]) != 2160 {
			t.Fatal("SDR penalty can block 1080p")
		}
	}
	second, err := mediaFilmsYAML(on, true)
	if err != nil || !bytes.Equal(second, on) {
		t.Fatalf("not idempotent: %v", err)
	}
	restored, err := mediaFilmsYAML(on, false)
	if err != nil || !bytes.Equal(restored, base) {
		t.Fatalf("off did not restore template: %v", err)
	}
	limited, err := mediaSizesYAML(on, SizeLimits{Films: 33, Films4K: 208})
	if err != nil || !strings.Contains(string(limited), "max: 208") || !strings.Contains(string(limited), "max: 33") {
		t.Fatalf("limits lost: %v", err)
	}
}

func filmFakeConfig(t *testing.T, f *mediaFake) (MediaSettingsConfig, *int) {
	t.Helper()
	f.resources["radarr/movie"] = []map[string]any{{"id": 1, "qualityProfileId": 3}}
	f.resources["seerr/settings/radarr"][0] = map[string]any{"id": 0, "hostname": "radarr", "is4k": false, "isDefault": true, "activeProfileId": 3, "activeProfileName": "HD Bluray + WEB", "apiKey": "private", "activeDirectory": "/data/media/movies"}
	// Match the owner's live HD codec score. The UHD sync below adds the
	// guide's no HDR/DV penalty before Iglo-OS copies the Dutch 4K profile.
	for i, name := range []string{"x265 (HD)", "x265 (no HDR/DV)"} {
		id, score := 70+i, 0
		if i == 0 {
			score = -10000
		}
		f.resources["radarr/customformat"] = append(f.resources["radarr/customformat"], map[string]any{"id": id, "name": name})
		profile := f.resources["radarr/qualityprofile"][0]
		profile["formatItems"] = append(profile["formatItems"].([]any), map[string]any{"format": id, "name": name, "score": score})
	}
	cfg := mediaFakeConfig(t, f)
	syncs := new(int)
	cfg.SyncRecyclarr = func(context.Context) error {
		*syncs = *syncs + 1
		data, err := os.ReadFile(cfg.RecyclarrPath)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), films4KProfile) && !slices.ContainsFunc(f.resources["radarr/qualityprofile"], func(p map[string]any) bool { return p["name"] == films4KProfile }) {
			// Simulate sync completion, without opening a listener or Docker.
			raw, _ := json.Marshal(f.resources["radarr/qualityprofile"][0])
			var profile map[string]any
			json.Unmarshal(raw, &profile)
			profile["id"], profile["name"] = 80, films4KProfile
			for _, raw := range profile["formatItems"].([]any) {
				item := raw.(map[string]any)
				if item["name"] == "x265 (no HDR/DV)" {
					item["score"] = -10000
				}
			}
			profile["cutoff"] = 19
			profile["items"] = []any{map[string]any{"name": "Bluray-2160p", "allowed": true}, map[string]any{"name": "Bluray-1080p", "allowed": true}}
			f.resources["radarr/qualityprofile"] = append(f.resources["radarr/qualityprofile"], profile)
		}
		return nil
	}
	return cfg, syncs
}

func TestFilmProfileSwitching(t *testing.T) {
	for _, inUse := range []bool{false, true} {
		t.Run(fmt.Sprint(inUse), func(t *testing.T) {
			f := newMediaFake()
			cfg, syncs := filmFakeConfig(t, f)
			var logs []string
			cfg.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
			settings := DefaultMediaSettings()
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			hd, _ := json.Marshal(f.resources["radarr/qualityprofile"])
			movies, _ := json.Marshal(f.resources["radarr/movie"])
			settings.UHDFilms = true
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			service := f.resources["seerr/settings/radarr"][0]
			if jsonInt(service["activeProfileId"]) != 80 || service["is4k"] != false || service["apiKey"] != "private" || service["activeDirectory"] != "/data/media/movies" {
				t.Fatalf("service: %v", service)
			}
			rules := f.resources["seerr/overrideRule"]
			var dutchID int
			for _, profile := range f.resources["radarr/qualityprofile"] {
				if profile["name"] == films4KProfile || profile["name"] == dutch4KProfile {
					assertFilmCodecPenalties(t, profile)
				}
				if profile["name"] == dutch4KProfile {
					dutchID = jsonInt(profile["id"])
					if profile["language"].(map[string]any)["name"] != "Dutch" || jsonInt(profile["cutoff"]) != 19 {
						t.Fatal("Dutch did not follow UHD")
					}
				}
			}
			if dutchID == 0 || !slices.ContainsFunc(rules, func(r map[string]any) bool {
				return r["radarrServiceId"] != nil && r["language"] == "nl" && jsonInt(r["profileId"]) == dutchID
			}) {
				t.Fatal("Dutch rule did not follow UHD")
			}
			if current, _ := json.Marshal(f.resources["radarr/qualityprofile"][:2]); !bytes.Equal(current, hd) {
				t.Fatal("changed existing HD or Dutch profile")
			}
			if current, _ := json.Marshal(f.resources["radarr/movie"]); !bytes.Equal(current, movies) {
				t.Fatal("changed existing films")
			}
			writes := len(f.writes)
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			if len(f.writes) != writes || *syncs != 1 {
				t.Fatalf("repeated save wrote or synced: %v, %d", f.writes[writes:], *syncs)
			}
			if inUse {
				f.resources["radarr/movie"] = append(f.resources["radarr/movie"], map[string]any{"qualityProfileId": 80}, map[string]any{"qualityProfileId": dutchID})
			}
			settings.UHDFilms = false
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			if jsonInt(f.resources["seerr/settings/radarr"][0]["activeProfileId"]) != 3 {
				t.Fatal("did not switch default to HD")
			}
			hasUHD := slices.ContainsFunc(f.resources["radarr/qualityprofile"], func(p map[string]any) bool { return p["name"] == films4KProfile || p["name"] == dutch4KProfile })
			if hasUHD != inUse {
				t.Fatalf("retained profiles: %v", f.resources["radarr/qualityprofile"])
			}
			if inUse && !strings.Contains(strings.Join(logs, "\n"), "because a film still uses it") {
				t.Fatal("kept profile without reporting it")
			}
			writes = len(f.writes)
			syncCount := *syncs
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			if len(f.writes) != writes || *syncs != syncCount {
				t.Fatal("repeated off save wrote or synced")
			}
			got, err := GetMediaSettings(cfg.SettingsPath)
			if err != nil || got.UHDFilms {
				t.Fatalf("saved off: %+v %v", got, err)
			}
		})
	}
}

func TestFilmProfileFailures(t *testing.T) {
	for _, failure := range []string{"sync", "missing profile", "seerr", "sizes", "save"} {
		t.Run(failure, func(t *testing.T) {
			f := newMediaFake()
			cfg, _ := filmFakeConfig(t, f)
			before, _ := os.ReadFile(cfg.RecyclarrPath)
			switch failure {
			case "sync":
				cfg.SyncRecyclarr = func(context.Context) error { return errors.New("offline") }
			case "missing profile":
				cfg.SyncRecyclarr = func(context.Context) error { return nil }
			case "seerr":
				f.fail = "PUT seerr/settings/radarr/0"
			case "sizes":
				f.fail = "GET sonarr/qualitydefinition"
			case "save":
				cfg.SettingsPath = filepath.Join(t.TempDir(), "missing", "settings.json")
			}
			settings := DefaultMediaSettings()
			settings.UHDFilms = true
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil {
				t.Fatal("expected failure")
			}
			after, _ := os.ReadFile(cfg.RecyclarrPath)
			if !bytes.Equal(before, after) {
				t.Fatal("failed switch changed config")
			}
			if jsonInt(f.resources["seerr/settings/radarr"][0]["activeProfileId"]) != 3 {
				t.Fatal("failed switch changed default")
			}
		})
	}
}

func TestFilmRenderKeepsStoredChoice(t *testing.T) {
	cfg, _ := filmFakeConfig(t, newMediaFake())
	settings := DefaultMediaSettings()
	settings.UHDFilms = true
	settings.Sizes.Films4K = 208
	data, _ := json.Marshal(savedMediaSettings{Version: 1, Settings: settings})
	if err := os.WriteFile(cfg.SettingsPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RenderMediaSizes("../../../deploy/stacks/arr/recyclarr.yml", cfg.RecyclarrPath, cfg.SettingsPath); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(cfg.RecyclarrPath)
	if !strings.Contains(string(got), films4KProfile) || !strings.Contains(string(got), "max: 208") {
		t.Fatalf("render: %s", got)
	}
}

// Keep unrelated service fields and user rules intact on every switch.
func TestFilmCleanupPreservesSeerrReferences(t *testing.T) {
	f := newMediaFake()
	cfg, _ := filmFakeConfig(t, f)
	settings := DefaultMediaSettings()
	settings.UHDFilms = true
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	f.resources["seerr/overrideRule"] = append(f.resources["seerr/overrideRule"], map[string]any{"id": 90, "radarrServiceId": 0, "keywords": "custom", "profileId": 80})
	settings.UHDFilms = false
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.resources["radarr/qualityprofile"], func(p map[string]any) bool { return p["name"] == films4KProfile }) {
		t.Fatal("deleted profile referenced by user rule")
	}
	for _, write := range f.writes {
		if strings.HasPrefix(write, http.MethodPut+" radarr/movie") {
			t.Fatal("changed a film")
		}
	}
}

func TestFilmFailureRestoresDutchRule(t *testing.T) {
	for _, previous := range []bool{false, true} {
		t.Run(fmt.Sprint(previous), func(t *testing.T) {
			f := newMediaFake()
			cfg, _ := filmFakeConfig(t, f)
			settings := DefaultMediaSettings()
			settings.UHDFilms = previous
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			rules, _ := json.Marshal(f.resources["seerr/overrideRule"])
			service, _ := json.Marshal(f.resources["seerr/settings/radarr"])
			settings.UHDFilms = !previous
			f.fail = "GET sonarr/qualitydefinition"
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil {
				t.Fatal("expected size read failure")
			}
			if got, _ := json.Marshal(f.resources["seerr/overrideRule"]); !bytes.Equal(got, rules) {
				t.Fatalf("rule was not restored: %s", got)
			}
			if got, _ := json.Marshal(f.resources["seerr/settings/radarr"]); !bytes.Equal(got, service) {
				t.Fatalf("default was not restored: %s", got)
			}
		})
	}
}

func assertFilmCodecPenalties(t *testing.T, profile map[string]any) {
	t.Helper()
	scores := map[string]int{}
	for _, raw := range profile["formatItems"].([]any) {
		item := raw.(map[string]any)
		scores[fmt.Sprint(item["name"])] = jsonInt(item["score"])
	}
	for _, name := range []string{"x265 (HD)", "x265 (no HDR/DV)"} {
		if score, ok := scores[name]; !ok || score != -10000 {
			t.Fatalf("%s: %s score %d, present %v", profile["name"], name, score, ok)
		}
	}
}

func TestFilmExistingDutch4KCodecPenalties(t *testing.T) {
	f := newMediaFake()
	cfg, _ := filmFakeConfig(t, f)
	settings := DefaultMediaSettings()
	settings.UHDFilms = true
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	for _, profile := range f.resources["radarr/qualityprofile"] {
		if profile["name"] != dutch4KProfile {
			continue
		}
		for _, raw := range profile["formatItems"].([]any) {
			item := raw.(map[string]any)
			if item["name"] == "x265 (HD)" || item["name"] == "x265 (no HDR/DV)" {
				item["score"] = 0
			}
		}
	}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	for _, profile := range f.resources["radarr/qualityprofile"] {
		if profile["name"] == dutch4KProfile {
			assertFilmCodecPenalties(t, profile)
		}
	}
	writes := len(f.writes)
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != writes {
		t.Fatal("corrected codec penalties are not idempotent")
	}
}
