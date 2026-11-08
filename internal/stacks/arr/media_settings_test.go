package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMediaSettingsValidation(t *testing.T) {
	if err := DefaultMediaSettings().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*MediaSettings)
	}{
		{"unknown language", func(s *MediaSettings) { s.Series.Subtitles = []string{"xx"} }},
		{"uppercase language", func(s *MediaSettings) { s.Films.Subtitles = []string{"EN"} }},
		{"empty language", func(s *MediaSettings) { s.Dutch.Subtitles = []string{""} }},
		{"long language", func(s *MediaSettings) { s.Anime.Subtitles = []string{"eng"} }},
		{"duplicate", func(s *MediaSettings) { s.Series.Subtitles = []string{"en", "en"} }},
		{"too many", func(s *MediaSettings) {
			s.Films.Subtitles = []string{"en", "nl", "fr", "de", "it", "es", "pt", "ja", "ko", "zh", "pl"}
		}},
		{"anime audio", func(s *MediaSettings) { s.Anime.Audio = "dutch" }},
		{"anime any", func(s *MediaSettings) { s.Anime.Audio = "any" }},
		{"dutch audio", func(s *MediaSettings) { s.Dutch.Audio = "original" }},
		{"dutch any", func(s *MediaSettings) { s.Dutch.Audio = "any" }},
		{"series audio", func(s *MediaSettings) { s.Series.Audio = "dutch" }},
		{"films audio", func(s *MediaSettings) { s.Films.Audio = "unknown" }},
		{"series any conflicts with Recyclarr", func(s *MediaSettings) { s.Series.Audio = "any" }},
		{"films any conflicts with Recyclarr", func(s *MediaSettings) { s.Films.Audio = "any" }},
		{"missing audio", func(s *MediaSettings) { s.Series.Audio = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := DefaultMediaSettings()
			tc.change(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("accepted invalid settings")
			}
		})
	}
	s := DefaultMediaSettings()
	s.Series.Subtitles = []string{"en", "nl", "fr", "de", "it", "es", "pt", "ja", "ko", "zh"}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeMediaSettings(t *testing.T) {
	data, _ := json.Marshal(DefaultMediaSettings())
	for name, input := range map[string]string{
		"unknown top field":    strings.TrimSuffix(string(data), "}") + `,"typo":true}`,
		"unknown nested field": strings.Replace(string(data), `"audio":"original"`, `"audio":"original","typo":true`, 1),
		"trailing object":      string(data) + ` {}`,
		"trailing garbage":     string(data) + ` false!`,
		"oversized":            string(data) + strings.Repeat(" ", 64*1024),
		"missing kinds":        `{"anime":{"subtitles":[],"audio":"original"}}`,
		"null":                 `null`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeMediaSettings(strings.NewReader(input)); err == nil {
				t.Fatal("accepted invalid JSON")
			}
		})
	}
	if _, err := DecodeMediaSettings(strings.NewReader(string(data) + "\n")); err != nil {
		t.Fatal(err)
	}
	// Exactly 64 KiB is allowed. Null lists normalize to arrays on output.
	input := string(data) + strings.Repeat(" ", 64*1024-len(data))
	if _, err := DecodeMediaSettings(strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	s, err := DecodeMediaSettings(strings.NewReader(strings.Replace(string(data), `"subtitles":[]`, `"subtitles":null`, 1)))
	if err != nil || s.Anime.Subtitles == nil {
		t.Fatalf("null list: %+v, %v", s, err)
	}
}

type mediaFake struct {
	mu             sync.Mutex
	resources      map[string][]map[string]any
	bazarrSettings map[string]map[string]any
	languages      []map[string]any
	titles         map[string][]map[string]any
	writes         []string
	fail           string
	batches        []int
	forms          []url.Values
}

func newMediaFake() *mediaFake {
	f := &mediaFake{resources: map[string][]map[string]any{}, titles: map[string][]map[string]any{}, bazarrSettings: map[string]map[string]any{
		"general": {"serie_default_enabled": true, "serie_default_profile": 1, "movie_default_enabled": true, "movie_default_profile": 1, "serie_tag_enabled": false, "movie_tag_enabled": false, "remove_profile_tags": []any{}},
		"sonarr":  {"excluded_series_types": []any{"anime", "daily"}, "excluded_tags": []any{"manual"}},
		"radarr":  {"excluded_tags": []any{"anime", "manual"}},
	}, languages: []map[string]any{{"code2": "en", "enabled": true}, {"code2": "nl", "enabled": false}, {"code2": "ja", "enabled": false}}}
	for i, app := range []string{"sonarr", "radarr"} {
		name := "WEB-1080p"
		if i == 1 {
			name = "HD Bluray + WEB"
		}
		f.resources[app+"/tag"] = []map[string]any{}
		f.resources[app+"/customformat"] = []map[string]any{{"id": 10, "name": notOriginalFormat}, {"id": 11, "name": "Good release"}}
		f.resources[app+"/qualityprofile"] = []map[string]any{{"id": 3, "name": name, "cutoff": 42, "items": []any{map[string]any{"id": 42, "name": "WEB 1080p", "allowed": true}}, "upgradeAllowed": true, "minFormatScore": 0, "cutoffFormatScore": 10000, "minUpgradeFormatScore": 1, "formatItems": []any{map[string]any{"format": 10, "name": notOriginalFormat, "score": -10000}, map[string]any{"format": 11, "name": "Good release", "score": 500}}, "language": map[string]any{"id": -2, "name": "Original"}}}
		if app == "radarr" {
			f.resources[app+"/customformat"] = f.resources[app+"/customformat"][1:]
			p := f.resources[app+"/qualityprofile"][0]
			p["formatItems"] = p["formatItems"].([]any)[1:]
		}
		f.resources["seerr/settings/"+app] = []map[string]any{{"id": 0, "hostname": app}}
	}
	raw, _ := bazarrLanguageProfile([]string{"en"})
	var profiles []map[string]any
	json.Unmarshal([]byte(raw), &profiles)
	// The real GET returns integers for originalFormat. Profile 1 must survive.
	profiles[0]["originalFormat"] = 0
	profiles = append(profiles, map[string]any{"profileId": 9, "name": "Manual", "items": []any{}, "cutoff": nil, "mustContain": []any{}, "mustNotContain": []any{}, "originalFormat": 1, "tag": "manual"})
	f.resources["bazarr/system/languages/profiles"] = profiles
	f.resources["seerr/overrideRule"] = []map[string]any{{"id": 1, "radarrServiceId": 0, "keywords": "210024", "profileId": 8, "tags": "8"}}
	f.titles["series"] = []map[string]any{{"sonarrSeriesId": 1, "profileId": 1, "tags": []string{}, "seriesType": "standard"}, {"sonarrSeriesId": 2, "profileId": 1, "tags": []string{"dutch"}, "seriesType": "standard"}, {"sonarrSeriesId": 3, "profileId": 1, "tags": []string{}, "seriesType": "anime"}, {"sonarrSeriesId": 4, "profileId": 9, "tags": []string{}, "seriesType": "standard"}}
	f.titles["movies"] = []map[string]any{{"radarrId": 1, "profileId": 1, "tags": []string{}}, {"radarrId": 2, "profileId": 1, "tags": []string{"dutch"}}, {"radarrId": 3, "profileId": 1, "tags": []string{"anime"}}}
	return f
}

func (f *mediaFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "bad path", 404)
		return
	}
	app := parts[0]
	resource := strings.Join(parts[3:], "/")
	if app == "bazarr" {
		resource = strings.Join(parts[2:], "/")
	}
	key := app + "/" + resource
	auth := "X-Api-Key"
	if app == "bazarr" {
		auth = "X-API-KEY"
	}
	if r.Header.Get(auth) != app+"-key" {
		http.Error(w, "bad API key", 401)
		return
	}
	if r.Method != "GET" {
		f.writes = append(f.writes, r.Method+" "+key)
	}
	if f.fail == r.Method+" "+key {
		http.Error(w, "forced failure", 500)
		return
	}
	if app == "bazarr" {
		switch resource {
		case "system/settings":
			if r.Method == "GET" {
				json.NewEncoder(w).Encode(f.bazarrSettings)
				return
			}
			r.ParseForm()
			f.forms = append(f.forms, r.Form)
			if raw := r.Form.Get("languages-profiles"); raw != "" {
				var profiles []map[string]any
				if err := json.Unmarshal([]byte(raw), &profiles); err != nil {
					http.Error(w, "bad profiles", 400)
					return
				}
				for _, p := range profiles {
					if p["originalFormat"] == false {
						p["originalFormat"] = float64(0)
					}
				}
				f.resources["bazarr/system/languages/profiles"] = profiles
			}
			for field, values := range r.Form {
				if field == "languages-enabled" {
					for _, lang := range f.languages {
						lang["enabled"] = slices.Contains(values, lang["code2"].(string))
					}
					continue
				}
				if !strings.HasPrefix(field, "settings-") {
					continue
				}
				keys := strings.SplitN(field, "-", 3)
				section, name := keys[1], keys[2]
				var value any = values[0]
				if strings.HasPrefix(name, "excluded_") || name == "remove_profile_tags" {
					arr := []any{}
					for _, v := range values {
						if v != "" {
							arr = append(arr, v)
						}
					}
					value = arr
				} else if values[0] == "true" {
					value = true
				} else if values[0] == "false" {
					value = false
				}
				f.bazarrSettings[section][name] = value
			}
			w.WriteHeader(204)
			return
		case "system/languages":
			json.NewEncoder(w).Encode(f.languages)
			return
		case "series", "movies":
			if r.Method == "GET" {
				json.NewEncoder(w).Encode(map[string]any{"data": f.titles[resource], "total": len(f.titles[resource])})
				return
			}
			r.ParseForm()
			field, idKey := "seriesid", "sonarrSeriesId"
			if resource == "movies" {
				field, idKey = "radarrid", "radarrId"
			}
			f.batches = append(f.batches, len(r.Form[field]))
			for i, id := range r.Form[field] {
				for _, title := range f.titles[resource] {
					if fmt.Sprint(title[idKey]) == id {
						value := r.Form["profileid"][i]
						if value == "null" {
							title["profileId"] = nil
						} else {
							var n int
							fmt.Sscan(value, &n)
							title["profileId"] = n
						}
					}
				}
			}
			w.WriteHeader(204)
			return
		}
	}
	if r.Method == "GET" {
		rows, ok := f.resources[key]
		if !ok {
			http.Error(w, "unexpected GET "+key, 404)
			return
		}
		json.NewEncoder(w).Encode(rows)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", 400)
		return
	}
	if r.Method == "POST" {
		body["id"] = float64(20 + len(f.resources[key]))
		f.resources[key] = append(f.resources[key], body)
		// Servarr adds new custom formats to all profiles with a zero score.
		if resource == "customformat" {
			for _, p := range f.resources[app+"/qualityprofile"] {
				p["formatItems"] = append(p["formatItems"].([]any), map[string]any{"format": body["id"], "name": body["name"], "score": 0})
			}
		}
		json.NewEncoder(w).Encode(body)
		return
	}
	if r.Method == "PUT" {
		collection := app + "/" + strings.Join(parts[3:len(parts)-1], "/")
		for i, existing := range f.resources[collection] {
			if fmt.Sprint(existing["id"]) == parts[len(parts)-1] {
				f.resources[collection][i] = body
				json.NewEncoder(w).Encode(body)
				return
			}
		}
	}
	http.Error(w, "unexpected mutation "+key, 404)
}

func mediaFakeConfig(t *testing.T, f *mediaFake) MediaSettingsConfig {
	t.Helper()
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return MediaSettingsConfig{Endpoints: Endpoints{Sonarr: server.URL + "/sonarr", Radarr: server.URL + "/radarr", Bazarr: server.URL + "/bazarr", Seerr: server.URL + "/seerr"}, SonarrAPIKey: "sonarr-key", RadarrAPIKey: "radarr-key", BazarrAPIKey: "bazarr-key", SeerrAPIKey: "seerr-key", SettingsPath: filepath.Join(t.TempDir(), "iglo-media.json"), HTTPClient: server.Client()}
}

// mediaHandlerConfig exercises requests without opening a local listener.
func mediaHandlerConfig(t *testing.T, handler http.Handler) MediaSettingsConfig {
	t.Helper()
	client := &http.Client{Transport: mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response.Result(), nil
	})}
	const base = "http://media.test"
	return MediaSettingsConfig{Endpoints: Endpoints{Sonarr: base + "/sonarr", Radarr: base + "/radarr", Bazarr: base + "/bazarr", Seerr: base + "/seerr"}, SonarrAPIKey: "sonarr-key", RadarrAPIKey: "radarr-key", BazarrAPIKey: "bazarr-key", SeerrAPIKey: "seerr-key", SettingsPath: filepath.Join(t.TempDir(), "iglo-media.json"), HTTPClient: client}
}

func TestApplyMediaSettings(t *testing.T) {
	for _, nonempty := range []bool{false, true} {
		t.Run(fmt.Sprint(nonempty), func(t *testing.T) {
			fake := newMediaFake()
			cfg := mediaFakeConfig(t, fake)
			settings := DefaultMediaSettings()
			if nonempty {
				settings.Dutch.Subtitles = []string{"nl", "en"}
				settings.Anime.Subtitles = []string{"ja", "en"}
			}
			if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(cfg.SettingsPath)
			if err != nil {
				t.Fatal(err)
			}
			if before.Mode().Perm() != 0644 {
				t.Fatalf("mode: %v", before.Mode())
			}
			writes := len(fake.writes)
			if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			if len(fake.writes) != writes {
				t.Errorf("second apply wrote: %v", fake.writes[writes:])
			}
			after, _ := os.Stat(cfg.SettingsPath)
			if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Error("rewrote unchanged settings file")
			}
			got, err := GetMediaSettings(cfg.SettingsPath)
			if err != nil || !sameJSON(settings, got) {
				t.Fatalf("get: %+v %v", got, err)
			}
			for _, app := range []string{"sonarr", "radarr"} {
				if tags := fake.resources[app+"/tag"]; len(tags) != 1 || tags[0]["label"] != "dutch" {
					t.Errorf("%s tags: %v", app, tags)
				}
				formats := fake.resources[app+"/customformat"]
				wantFormats := 4
				if app == "radarr" {
					wantFormats = 3
				}
				if len(formats) != wantFormats {
					t.Fatalf("formats: %v", formats)
				}
				for _, f := range formats {
					if f["name"] == dutchAudioFormat && !matchesDutchFormat(f, false) {
						t.Error("wrong Dutch spec")
					}
					if f["name"] == notDutchAudioFormat && !matchesDutchFormat(f, true) {
						t.Error("wrong negated spec")
					}
				}
				profiles := fake.resources[app+"/qualityprofile"]
				if len(profiles) != 2 {
					t.Fatalf("profiles: %v", profiles)
				}
				if app == "radarr" && jsonInt(profiles[1]["language"].(map[string]any)["id"]) != 7 {
					t.Error("Radarr Dutch dubs blocked by profile language")
				}
				if !sameJSON(profiles[0]["items"], profiles[1]["items"]) || !sameJSON(profiles[0]["cutoff"], profiles[1]["cutoff"]) {
					t.Error("lost source qualities or cutoff")
				}
				scores := map[string]int{}
				for _, item := range profiles[1]["formatItems"].([]any) {
					f := item.(map[string]any)
					scores[fmt.Sprint(f["name"])] = jsonInt(f["score"])
				}
				if scores[notOriginalFormat] != 0 || scores[dutchAudioFormat] != 100 || scores[notDutchAudioFormat] != -10000 || scores["Good release"] != 500 {
					t.Errorf("scores: %v", scores)
				}
				first := profiles[0]["formatItems"].([]any)[0].(map[string]any)
				wantScore := -10000
				if app == "radarr" {
					wantScore = 500
				}
				if jsonInt(first["score"]) != wantScore {
					t.Error("changed Recyclarr source score")
				}
			}
			profiles := fake.resources["bazarr/system/languages/profiles"]
			want := 2
			if nonempty {
				want = 4
			}
			if len(profiles) != want {
				t.Fatalf("Bazarr profiles: %v", profiles)
			}
			if profiles[0]["name"] != "Default" || profiles[1]["name"] != "Manual" {
				t.Error("lost existing Bazarr profiles")
			}
			for _, section := range []string{"sonarr", "radarr"} {
				tags := fake.bazarrSettings[section]["excluded_tags"].([]any)
				if slices.Contains(tags, any("dutch")) == nonempty || !slices.Contains(tags, any("manual")) {
					t.Errorf("%s exclusions: %v", section, tags)
				}
			}
			types := fake.bazarrSettings["sonarr"]["excluded_series_types"].([]any)
			if slices.Contains(types, any("anime")) == nonempty || !slices.Contains(types, any("daily")) {
				t.Errorf("series types: %v", types)
			}
			if fake.bazarrSettings["general"]["serie_tag_enabled"] != nonempty || fake.bazarrSettings["general"]["movie_tag_enabled"] != nonempty {
				t.Error("tag profiles disabled")
			}
			if len(fake.resources["seerr/overrideRule"]) != 3 {
				t.Fatalf("rules: %v", fake.resources["seerr/overrideRule"])
			}
			for _, r := range fake.resources["seerr/overrideRule"][1:] {
				if r["language"] != "nl" || r["tags"] != "20" || jsonInt(r["profileId"]) != 21 {
					t.Errorf("Dutch rule: %v", r)
				}
			}
			if fake.titles["series"][3]["profileId"] != 9 {
				t.Error("overwrote manual subtitle profile")
			}
			for _, title := range fake.titles["series"][1:3] {
				if (nonempty && jsonInt(title["profileId"]) <= 1) || (!nonempty && jsonInt(title["profileId"]) != 1) {
					t.Errorf("special title: %v", title)
				}
			}
		})
	}
}

func TestMediaSettingsTransitionsAndExistingObjects(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaHandlerConfig(t, fake)
	settings := DefaultMediaSettings()
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	// Recyclarr can later change its source. Existing Dutch profiles retain
	// their own qualities and unrelated scores. Owned scores are repaired.
	for _, app := range []string{"sonarr", "radarr"} {
		profiles := fake.resources[app+"/qualityprofile"]
		profiles[0]["cutoff"] = 99
		for _, raw := range profiles[1]["formatItems"].([]any) {
			item := raw.(map[string]any)
			if item["name"] == "Good release" {
				item["score"] = 777
			}
			if item["name"] == dutchAudioFormat {
				item["score"] = 0
			}
		}
	}
	// Repair an edited rule without dropping extra tags or adding duplicates.
	fake.resources["seerr/overrideRule"][1]["tags"] = "91"
	fake.resources["seerr/overrideRule"][1]["profileId"] = 0
	// Repair an edited custom format too.
	fake.resources["sonarr/customformat"][2]["specifications"].([]any)[0].(map[string]any)["negate"] = true
	fake.bazarrSettings["general"]["remove_profile_tags"] = []any{"dutch", "anime", "manual"}
	settings.Dutch.Subtitles = []string{"nl"}
	settings.Anime.Subtitles = []string{"en"}
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	removeTags := fake.bazarrSettings["general"]["remove_profile_tags"].([]any)
	if len(removeTags) != 1 || removeTags[0] != "manual" {
		t.Errorf("owned profiles disabled by remove tags: %v", removeTags)
	}
	if len(fake.resources["seerr/overrideRule"]) != 3 || fake.resources["seerr/overrideRule"][1]["tags"] != "91,20" {
		t.Error("failed to repair existing rule")
	}
	for _, app := range []string{"sonarr", "radarr"} {
		profile := fake.resources[app+"/qualityprofile"][1]
		if jsonInt(profile["cutoff"]) != 42 {
			t.Error("replaced existing Dutch cutoff")
		}
		for _, raw := range profile["formatItems"].([]any) {
			item := raw.(map[string]any)
			if item["name"] == "Good release" && jsonInt(item["score"]) != 777 {
				t.Error("changed unrelated score")
			}
			if item["name"] == dutchAudioFormat && jsonInt(item["score"]) != 100 {
				t.Error("failed to repair owned score")
			}
		}
	}
	settings.Dutch.Subtitles = []string{}
	settings.Anime.Subtitles = []string{}
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	for _, p := range fake.resources["bazarr/system/languages/profiles"] {
		if (p["name"] == "Anime (Iglo-OS)" || p["name"] == dutchProfile) && p["tag"] != nil {
			t.Errorf("empty kind kept tag: %v", p)
		}
	}
	if jsonInt(fake.titles["series"][0]["profileId"]) != 1 {
		t.Error("cleared Series profile")
	}
	if jsonInt(fake.titles["movies"][0]["profileId"]) != 1 {
		t.Error("changed the shared Films profile")
	}
	if languages, err := mediaProfileLanguages(fake.resources["bazarr/system/languages/profiles"][0]); err != nil || !slices.Equal(languages, []string{"en"}) {
		t.Fatalf("changed normal languages: %v %v", languages, err)
	}
	count := len(fake.writes)
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	if len(fake.writes) != count {
		t.Errorf("optional exclusions not idempotent: %v", fake.writes[count:])
	}
}

func TestMediaSettingsFailures(t *testing.T) {
	for _, failure := range []string{"POST sonarr/customformat", "GET radarr/qualityprofile", "POST bazarr/system/settings", "POST bazarr/series", "POST bazarr/movies"} {
		t.Run(failure, func(t *testing.T) {
			fake := newMediaFake()
			cfg := mediaFakeConfig(t, fake)
			fake.fail = failure
			settings := DefaultMediaSettings()
			settings.Anime.Subtitles = []string{"en"}
			if err := ApplyMediaSettings(context.Background(), settings, cfg); err == nil {
				t.Fatal("ignored API failure")
			}
			if _, err := os.Stat(cfg.SettingsPath); !os.IsNotExist(err) {
				t.Fatal("saved failed apply")
			}
			fake.fail = ""
			if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
				t.Fatalf("retry: %v", err)
			}
		})
	}
	fake := newMediaFake()
	cfg := mediaFakeConfig(t, fake)
	settings := DefaultMediaSettings()
	settings.Series.Audio = "any"
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err == nil || len(fake.writes) != 0 {
		t.Fatal("invalid input changed apps")
	}
}

func TestMediaSettingsFileAndKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "iglo-media.json")
	settings, err := GetMediaSettings(path)
	if !errors.Is(err, ErrMediaSettingsUnreadable) {
		t.Fatalf("missing: %+v %v", settings, err)
	}
	for _, data := range []string{`{"version":2}`, `{"version":1,"settings":{}}`, `not JSON`} {
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := GetMediaSettings(path); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	fake := newMediaFake()
	cfg := mediaFakeConfig(t, fake)
	cfg.BazarrAPIKey = ""
	cfg.SeerrAPIKey = ""
	cfg.BazarrConfigPath = filepath.Join(t.TempDir(), "config.yaml")
	cfg.SeerrConfigPath = filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(cfg.BazarrConfigPath, []byte("auth:\n  apikey: bazarr-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.SeerrConfigPath, []byte(`{"main":{"apiKey":"seerr-key"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyMediaSettings(context.Background(), DefaultMediaSettings(), cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg.SettingsPath)
	if strings.Contains(string(data), "-key") {
		t.Fatal("credentials saved")
	}
	state, bazarr, seerr := MediaSettingsPaths("/some/stack/answers")
	if state != "/some/stack/iglo-media.json" || bazarr != "/some/stack/config/bazarr/config/config.yaml" || seerr != "/some/stack/config/seerr/settings.json" {
		t.Fatalf("paths: %s %s %s", state, bazarr, seerr)
	}
}

func TestMediaBazarrProfilesAndManualAssignments(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaFakeConfig(t, fake)
	profile := fake.resources["bazarr/system/languages/profiles"][0]
	profile["name"], profile["cutoff"] = "My series", 65535
	item := profile["items"].([]any)[0].(map[string]any)
	item["hi"], item["forced"], item["audio_exclude"] = "True", "True", "True"
	for _, kind := range []string{"series", "movies"} {
		idKey := "sonarrSeriesId"
		if kind == "movies" {
			idKey = "radarrId"
		}
		for _, tag := range []string{"anime", "dutch", ""} {
			for _, current := range []any{nil, 9} {
				fake.titles[kind] = append(fake.titles[kind], map[string]any{idKey: len(fake.titles[kind]) + 1, "profileId": current, "tags": []string{tag}, "seriesType": tag})
			}
		}
	}
	before := map[string][]byte{}
	for _, kind := range []string{"series", "movies"} {
		for i, title := range fake.titles[kind] {
			if title["profileId"] == nil || jsonInt(title["profileId"]) == 9 {
				data, _ := json.Marshal(title)
				before[fmt.Sprintf("%s/%d", kind, i)] = data
			}
		}
	}
	settings := DefaultMediaSettings()
	settings.Series.Subtitles = []string{"nl", "en"}
	settings.Films.Subtitles = []string{"ja"}
	settings.Anime.Subtitles, settings.Dutch.Subtitles = []string{"en"}, []string{"nl"}
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	profiles := fake.resources["bazarr/system/languages/profiles"]
	if len(profiles) != 5 {
		t.Fatalf("profiles: %v", profiles)
	}
	series := profiles[0]
	if jsonInt(series["profileId"]) != 1 || series["name"] != "My series" || jsonInt(series["cutoff"]) != 65535 {
		t.Fatalf("lost profile fields: %v", series)
	}
	items := series["items"].([]any)
	en := items[0].(map[string]any)
	if en["language"] != "en" || en["hi"] != "True" || en["forced"] != "True" || en["audio_exclude"] != "True" {
		t.Fatalf("lost flags: %v", en)
	}
	nl := items[1].(map[string]any)
	if nl["language"] != "nl" || nl["hi"] != "False" || nl["forced"] != "False" || nl["audio_exclude"] != "False" {
		t.Fatalf("new language inherited flags: %v", nl)
	}
	filmsID := 0
	for _, p := range profiles {
		if p["name"] == "Series (Iglo-OS)" {
			t.Fatal("created a Series profile")
		}
		if p["name"] == "Films (Iglo-OS)" {
			filmsID = jsonInt(p["profileId"])
		}
	}
	if filmsID == 0 || jsonInt(fake.titles["movies"][0]["profileId"]) != filmsID || jsonInt(fake.titles["series"][0]["profileId"]) != 1 {
		t.Fatal("wrong normal assignments")
	}
	settings.Films.Subtitles = []string{"en"}
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	profiles = fake.resources["bazarr/system/languages/profiles"]
	if len(profiles) != 5 {
		t.Fatal("duplicated Films profile")
	}
	for _, p := range profiles {
		if jsonInt(p["profileId"]) == filmsID {
			languages, err := mediaProfileLanguages(p)
			if err != nil || !slices.Equal(languages, []string{"en"}) {
				t.Fatalf("did not update Films profile: %v %v", p, err)
			}
		}
	}
	settings.Films.Subtitles = slices.Clone(settings.Series.Subtitles)
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	if jsonInt(fake.titles["movies"][0]["profileId"]) != 1 {
		t.Fatal("equal Films languages did not return to profile 1")
	}
	for _, kind := range []string{"series", "movies"} {
		for i, title := range fake.titles[kind] {
			if data, ok := before[fmt.Sprintf("%s/%d", kind, i)]; ok {
				got, _ := json.Marshal(title)
				if string(got) != string(data) {
					t.Fatalf("changed manual title: %s -> %s", data, got)
				}
			}
		}
	}
}

func TestGetMediaSettingsReadsBazarr(t *testing.T) {
	for _, scenario := range []string{"live", "excluded", "no Dutch profile", "no Anime profile", "missing profile 1", "unreachable"} {
		t.Run(scenario, func(t *testing.T) {
			fake := newMediaFake()
			cfg := mediaFakeConfig(t, fake)
			cfg.EnvPath = filepath.Join(t.TempDir(), "stack.vars")
			raw, _ := bazarrLanguageProfile([]string{"nl", "en"})
			var profiles []map[string]any
			if err := json.Unmarshal([]byte(raw), &profiles); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Anime", "Dutch"} {
				p := maps.Clone(profiles[0])
				p["profileId"], p["name"] = len(profiles)+1, name+" (Iglo-OS)"
				if name == "Anime" {
					p["items"] = []any{map[string]any{"id": 1, "language": "ja"}}
				} else {
					p["items"] = []any{map[string]any{"id": 1, "language": "nl"}}
				}
				profiles = append(profiles, p)
			}
			fake.resources["bazarr/system/languages/profiles"] = profiles
			fake.bazarrSettings["sonarr"]["excluded_series_types"] = []any{}
			if scenario == "excluded" {
				fake.bazarrSettings["sonarr"]["excluded_series_types"] = []any{"anime"}
				fake.bazarrSettings["radarr"]["excluded_tags"] = []any{"dutch"}
			}
			if scenario == "no Dutch profile" {
				fake.resources["bazarr/system/languages/profiles"] = profiles[:2]
			}
			if scenario == "no Anime profile" {
				fake.resources["bazarr/system/languages/profiles"] = append(profiles[:1:1], profiles[2:]...)
			}
			if scenario == "missing profile 1" {
				fake.resources["bazarr/system/languages/profiles"] = profiles[1:]
			}
			if scenario == "unreachable" {
				fake.fail = "GET bazarr/system/settings"
			}
			got, err := GetMediaSettings(cfg.SettingsPath, cfg)
			if scenario == "unreachable" {
				if err != ErrMediaSettingsUnavailable || err.Error() != "Bazarr is not answering yet. Try again in a minute." {
					t.Fatalf("unavailable: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"nl", "en"}
			if scenario == "missing profile 1" {
				want = []string{"en"}
			}
			if !slices.Equal(got.Series.Subtitles, want) || !slices.Equal(got.Films.Subtitles, want) {
				t.Fatalf("normal: %+v", got)
			}
			if scenario == "live" && (!slices.Equal(got.Anime.Subtitles, []string{"ja"}) || !slices.Equal(got.Dutch.Subtitles, []string{"nl"})) {
				t.Fatalf("tagged: %+v", got)
			}
			if scenario == "excluded" && (len(got.Anime.Subtitles) != 0 || len(got.Dutch.Subtitles) != 0) {
				t.Fatalf("excluded: %+v", got)
			}
			if scenario == "no Anime profile" && len(got.Anime.Subtitles) != 0 {
				t.Fatal("invented Anime subtitles")
			}
			if scenario == "no Dutch profile" && len(got.Dutch.Subtitles) != 0 {
				t.Fatal("invented Dutch subtitles")
			}

		})
	}
}

func TestMediaSeerrWarningsStillSave(t *testing.T) {
	for _, scenario := range []string{"missing sonarr", "missing radarr", "GET seerr/settings/sonarr", "GET seerr/overrideRule", "POST seerr/overrideRule", "missing key", "PUT seerr/overrideRule/21"} {
		t.Run(scenario, func(t *testing.T) {
			fake := newMediaFake()
			cfg := mediaFakeConfig(t, fake)
			if strings.HasPrefix(scenario, "PUT") {
				if err := ApplyMediaSettings(context.Background(), DefaultMediaSettings(), cfg); err != nil {
					t.Fatal(err)
				}
				fake.resources["seerr/overrideRule"][1]["profileId"] = 0
			}
			if strings.HasPrefix(scenario, "missing ") && scenario != "missing key" {
				fake.resources["seerr/settings/"+strings.TrimPrefix(scenario, "missing ")] = nil
			} else {
				fake.fail = scenario
			}
			if scenario == "missing key" {
				cfg.SeerrAPIKey, cfg.SeerrConfigPath = "", filepath.Join(t.TempDir(), "missing.json")
			}
			var logs []string
			cfg.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
			settings := DefaultMediaSettings()
			settings.Series.Subtitles, settings.Films.Subtitles = []string{"nl"}, []string{"nl"}
			if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			got, err := GetMediaSettings(cfg.SettingsPath)
			if err != nil || !sameJSON(got, settings) {
				t.Fatalf("not saved: %+v %v", got, err)
			}
			warning := false
			for _, line := range logs {
				warning = warning || strings.HasPrefix(line, "Could not add the Dutch rule in Seerr: ") && strings.HasSuffix(line, ". Add it in Seerr under Settings, Override Rules.")
			}
			if !warning || logs[len(logs)-1] != "Saving the media settings" {
				t.Fatalf("logs: %v", logs)
			}
		})
	}
}

func TestMediaSeerrLeavesHandmadeRules(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			fake := newMediaFake()
			cfg := mediaFakeConfig(t, fake)
			if after {
				if err := ApplyMediaSettings(context.Background(), DefaultMediaSettings(), cfg); err != nil {
					t.Fatal(err)
				}
				fake.resources["seerr/overrideRule"][1]["profileId"] = 0
			}
			manual := map[string]any{"id": 80, "sonarrServiceId": 0, "language": "nl", "profileId": 9, "tags": "manual"}
			before, _ := json.Marshal(manual)
			fake.resources["seerr/overrideRule"] = append(fake.resources["seerr/overrideRule"], manual)
			var logs []string
			cfg.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
			if err := ApplyMediaSettings(context.Background(), DefaultMediaSettings(), cfg); err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(manual)
			if string(got) != string(before) {
				t.Fatalf("rewrote hand-made rule: %s", got)
			}
			if !strings.Contains(strings.Join(logs, "\n"), "another Dutch sonarr rule exists") {
				t.Fatalf("no warning: %v", logs)
			}
			rules := fake.resources["seerr/overrideRule"]
			want := 3
			if after {
				want = 4
				if jsonInt(rules[1]["profileId"]) != 21 {
					t.Fatal("did not repair owned rule")
				}
			}
			if len(rules) != want {
				t.Fatalf("extra rules: %v", rules)
			}
			saved, err := readMediaSettings(cfg.SettingsPath)
			if err != nil {
				t.Fatal(err)
			}
			if (saved.SeerrRuleIDs["sonarr"] > 0) != after || saved.SeerrRuleIDs["radarr"] <= 0 {
				t.Fatalf("ownership: %v", saved.SeerrRuleIDs)
			}
		})
	}
}

type mediaRoundTripper func(*http.Request) (*http.Response, error)

func (f mediaRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMediaBazarrBatchSizeAndTimeouts(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaFakeConfig(t, fake)
	for _, kind := range []string{"series", "movies"} {
		fake.titles[kind] = nil
		key := "sonarrSeriesId"
		if kind == "movies" {
			key = "radarrId"
		}
		for i := range 123 {
			fake.titles[kind] = append(fake.titles[kind], map[string]any{key: i + 1, "profileId": 1, "tags": []string{"dutch"}})
		}
	}
	cfg.HTTPClient.Timeout = 5 * time.Second
	transport := cfg.HTTPClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	longCalls := 0
	cfg.HTTPClient.Transport = mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
		long := r.Method == "POST" && (strings.HasSuffix(r.URL.Path, "/series") || strings.HasSuffix(r.URL.Path, "/movies"))
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/bazarr/api/system/settings") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			long = strings.Contains(string(body), "languages-profiles=")
		}
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Error("no client timeout")
		} else if long {
			longCalls++
			if remaining := time.Until(deadline); remaining < 9*time.Minute || remaining > 10*time.Minute {
				t.Errorf("write timeout: %v", remaining)
			}
		} else if time.Until(deadline) > 5*time.Second {
			t.Errorf("extended ordinary call: %s", r.URL.Path)
		}
		return transport.RoundTrip(r)
	})
	settings := DefaultMediaSettings()
	settings.Dutch.Subtitles = []string{"nl"}
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(fake.batches, []int{50, 50, 23, 50, 50, 23}) || longCalls != 7 {
		t.Fatalf("batches %v, long calls %d", fake.batches, longCalls)
	}
	if cfg.HTTPClient.Timeout != 5*time.Second {
		t.Fatal("mutated shared timeout")
	}
	for _, kind := range []string{"series", "movies"} {
		for _, title := range fake.titles[kind] {
			if jsonInt(title["profileId"]) <= 1 {
				t.Fatalf("missed title: %v", title)
			}
		}
	}
	// A settings edit without a profile rewrite keeps the normal timeout.
	fake.bazarrSettings["general"]["serie_tag_enabled"] = false
	before := longCalls
	if err := ApplyMediaSettings(context.Background(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	if longCalls != before {
		t.Fatal("extended a settings-only call")
	}
}

func TestMediaMalformedFormatItems(t *testing.T) {
	for _, value := range []any{"wrong type", []any{"odd item"}} {
		fake := newMediaFake()
		cfg := mediaFakeConfig(t, fake)
		// Skip custom format creation so the fake can return malformed profile data.
		app := newServarr("sonarr", cfg.Endpoints.Sonarr, "v3", cfg.SonarrAPIKey, cfg.HTTPClient)
		if _, err := applyMediaServarr(context.Background(), app, "WEB-1080p"); err != nil {
			t.Fatal(err)
		}
		fake.resources["sonarr/qualityprofile"][1]["formatItems"] = value
		if _, err := applyMediaServarr(context.Background(), app, "WEB-1080p"); err == nil || !strings.Contains(err.Error(), "format") {
			t.Fatalf("accepted malformed items: %v", err)
		}
	}
}

func TestMediaBazarrInvalidProfileLanguage(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaFakeConfig(t, fake)
	settings := DefaultMediaSettings()
	settings.Anime.Subtitles = []string{"invalid"}
	b := &bazarr{baseURL: cfg.Endpoints.Bazarr, apiKey: cfg.BazarrAPIKey, http: cfg.HTTPClient}
	if err := applyMediaBazarr(context.Background(), b, settings); err == nil || !strings.Contains(err.Error(), "unsupported Bazarr subtitle language") {
		t.Fatalf("ignored profile builder error: %v", err)
	}
	if len(fake.writes) != 0 {
		t.Fatal("invalid profile changed Bazarr")
	}
}

func TestMediaBazarrDuplicateItemsRoundTrip(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaFakeConfig(t, fake)
	profile := fake.resources["bazarr/system/languages/profiles"][0]
	normal := profile["items"].([]any)[0].(map[string]any)
	forced := maps.Clone(normal)
	forced["id"], forced["forced"], forced["hi"], forced["audio_exclude"] = 2, "True", "True", "True"
	profile["items"] = []any{normal, forced}
	got, err := GetMediaSettings(cfg.SettingsPath, cfg)
	if err != nil || !slices.Equal(got.Series.Subtitles, []string{"en"}) {
		t.Fatalf("GET duplicates: %+v %v", got, err)
	}
	data, _ := json.Marshal(got)
	if _, err := DecodeMediaSettings(strings.NewReader(string(data))); err != nil {
		t.Fatal(err)
	}
	got.Series.Subtitles = []string{"nl", "en"}
	got.Films.Subtitles = slices.Clone(got.Series.Subtitles)
	if err := ApplyMediaSettings(t.Context(), got, cfg); err != nil {
		t.Fatal(err)
	}
	items := fake.resources["bazarr/system/languages/profiles"][0]["items"].([]any)
	if len(items) != 3 || !sameJSON(items[0], normal) || !sameJSON(items[1], forced) {
		t.Fatalf("lost existing variants or order: %v", items)
	}
	added := items[2].(map[string]any)
	if added["language"] != "nl" || jsonInt(added["id"]) != 3 {
		t.Fatalf("new code not appended: %v", added)
	}
	for _, flag := range []string{"hi", "forced", "audio_exclude", "audio_only_include"} {
		if added[flag] != "False" {
			t.Fatalf("new flag %s: %v", flag, added)
		}
	}
}

func TestMediaBazarrCutoff(t *testing.T) {
	for _, cutoff := range []any{nil, 1, 2, 65535} {
		t.Run(fmt.Sprint(cutoff), func(t *testing.T) {
			profile := map[string]any{"cutoff": cutoff, "items": []any{
				map[string]any{"id": 1, "language": "en", "forced": "True"},
				map[string]any{"id": 2, "language": "nl", "forced": "False"},
			}}
			got, err := mediaBazarrProfile(profile, []string{"nl", "fr"})
			if err != nil {
				t.Fatal(err)
			}
			want := cutoff
			if cutoff == 1 {
				want = nil
			}
			if !sameJSON(got["cutoff"], want) || profile["cutoff"] != cutoff {
				t.Fatalf("cutoff = %v, want %v; source = %v", got["cutoff"], want, profile)
			}
		})
	}
}

func TestMediaGetUnavailableAndSaved(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaFakeConfig(t, fake)
	fake.fail = "GET bazarr/system/settings"
	if _, err := GetMediaSettings(cfg.SettingsPath, cfg); err != ErrMediaSettingsUnavailable {
		t.Fatalf("no saved file: %v", err)
	}
	want := DefaultMediaSettings()
	want.Series.Subtitles = []string{"nl"}
	data, _ := json.Marshal(savedMediaSettings{Version: 1, Settings: want})
	if err := os.WriteFile(cfg.SettingsPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := GetMediaSettings(cfg.SettingsPath, cfg)
	if err != nil || !sameJSON(got, want) {
		t.Fatalf("saved file: %+v %v", got, err)
	}
}

func TestMediaBazarrSharedLanguagesInAnotherOrder(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaHandlerConfig(t, fake)
	profile := fake.resources["bazarr/system/languages/profiles"][0]
	profile["items"] = append(profile["items"].([]any), map[string]any{"id": 2, "language": "nl"})
	before, _ := json.Marshal(fake.titles["movies"])
	settings := DefaultMediaSettings()
	settings.Series.Subtitles = []string{"nl", "en"}
	settings.Films.Subtitles = []string{"en", "nl"}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(fake.titles["movies"], json.RawMessage(before)) {
		t.Fatal("moved films for unchanged languages")
	}
	for _, p := range fake.resources["bazarr/system/languages/profiles"] {
		if p["name"] == "Films (Iglo-OS)" {
			t.Fatal("made an unneeded Films profile")
		}
	}
	for _, form := range fake.forms {
		if form.Has("languages-profiles") {
			t.Fatal("rewrote profiles for a different selection order")
		}
	}
}

func TestMediaBazarrSeriesChangeKeepsFilmsSetting(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaHandlerConfig(t, fake)
	settings := DefaultMediaSettings()
	settings.Films.Subtitles = []string{"fr"}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	filmID := jsonInt(fake.titles["movies"][0]["profileId"])
	settings.Series.Subtitles = []string{"nl"}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	for _, p := range fake.resources["bazarr/system/languages/profiles"] {
		if p["name"] == "Films (Iglo-OS)" {
			languages, err := mediaProfileLanguages(p)
			if err != nil || !slices.Equal(languages, []string{"fr"}) || jsonInt(p["profileId"]) != filmID {
				t.Fatalf("Series changed the Films profile: %v %v", p, err)
			}
		}
	}
	if jsonInt(fake.titles["movies"][0]["profileId"]) != filmID {
		t.Fatal("Series moved films away from their profile")
	}
}

func TestMediaBazarrDefaultProfiles(t *testing.T) {
	for _, previous := range []any{nil, "", 1, "1", 9, "9"} {
		t.Run(fmt.Sprint(previous), func(t *testing.T) {
			fake := newMediaFake()
			cfg := mediaFakeConfig(t, fake)
			fake.bazarrSettings["general"]["serie_default_profile"] = previous
			fake.bazarrSettings["general"]["movie_default_profile"] = previous
			settings := DefaultMediaSettings()
			settings.Films.Subtitles = []string{"nl"}
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			general := fake.bazarrSettings["general"]
			if fmt.Sprint(previous) == "9" {
				if general["serie_default_profile"] != previous || general["movie_default_profile"] != previous {
					t.Fatalf("lost custom defaults: %v", general)
				}
			} else {
				if fmt.Sprint(general["serie_default_profile"]) != "1" || fmt.Sprint(general["movie_default_profile"]) != "10" {
					t.Fatalf("missing stack defaults: %v", general)
				}
				settings.Films.Subtitles = []string{"en"}
				if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
					t.Fatal(err)
				}
				if fmt.Sprint(general["movie_default_profile"]) != "1" {
					t.Fatalf("did not return owned default to shared profile: %v", general)
				}
			}
		})
	}
}

func TestMediaBazarrTagSwitches(t *testing.T) {
	for _, initial := range []bool{false, true} {
		for _, kind := range []string{"empty", "Anime", "Dutch"} {
			t.Run(fmt.Sprintf("%v/%s", initial, kind), func(t *testing.T) {
				fake := newMediaFake()
				cfg := mediaFakeConfig(t, fake)
				general := fake.bazarrSettings["general"]
				general["serie_tag_enabled"], general["movie_tag_enabled"] = initial, initial
				settings := DefaultMediaSettings()
				if kind == "Anime" {
					settings.Anime.Subtitles = []string{"en"}
				} else if kind == "Dutch" {
					settings.Dutch.Subtitles = []string{"nl"}
				}
				if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
					t.Fatal(err)
				}
				want := initial || kind != "empty"
				if general["serie_tag_enabled"] != want || general["movie_tag_enabled"] != want {
					t.Fatalf("tag switches: %v", general)
				}
			})
		}
	}
}

func TestMediaRealHostRoundTrip(t *testing.T) {
	fake := newMediaFake()
	fake.resources["sonarr/tag"] = []map[string]any{{"id": 7, "label": "dutch"}}
	fake.resources["bazarr/system/languages/profiles"] = fake.resources["bazarr/system/languages/profiles"][:1]
	fake.bazarrSettings["sonarr"]["excluded_tags"] = []any{"dutch"}
	fake.bazarrSettings["sonarr"]["excluded_series_types"] = []any{"anime"}
	fake.bazarrSettings["radarr"]["excluded_tags"] = []any{"anime"}
	for _, titles := range fake.titles {
		for _, title := range titles {
			title["profileId"] = 1
		}
	}
	cfg := mediaFakeConfig(t, fake)
	before, _ := json.Marshal(fake.resources["bazarr/system/languages/profiles"][0])
	settings, err := GetMediaSettings(cfg.SettingsPath, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.Anime.Subtitles) != 0 || len(settings.Dutch.Subtitles) != 0 {
		t.Fatalf("invented tagged languages: %+v", settings)
	}
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	if len(fake.resources["sonarr/tag"]) != 1 || jsonInt(fake.resources["sonarr/tag"][0]["id"]) != 7 || slices.Contains(fake.writes, "POST sonarr/tag") {
		t.Fatalf("did not reuse tag 7: %v", fake.writes)
	}
	if !sameJSON(fake.resources["bazarr/system/languages/profiles"][0], json.RawMessage(before)) {
		t.Fatal("changed profile 1")
	}
	if slices.Contains(fake.writes, "POST bazarr/series") || slices.Contains(fake.writes, "POST bazarr/movies") {
		t.Fatalf("assigned titles: %v", fake.writes)
	}
	for _, form := range fake.forms {
		if form.Has("languages-profiles") {
			t.Fatalf("posted language profiles on unchanged settings: %v", form)
		}
	}
	if !mediaContains(fake.bazarrSettings["radarr"]["excluded_tags"], "dutch") {
		t.Fatal("Radarr exclusions did not gain dutch")
	}
	for _, app := range []string{"sonarr", "radarr"} {
		found := false
		for _, rule := range fake.resources["seerr/overrideRule"] {
			if rule[app+"ServiceId"] != nil && rule["language"] == "nl" {
				found = true
				if app == "sonarr" && rule["tags"] != "7" {
					t.Fatalf("rule did not reuse tag 7: %v", rule)
				}
			}
		}
		if !found {
			t.Fatalf("Seerr has no %s Dutch rule: %v", app, fake.resources["seerr/overrideRule"])
		}
	}
}

func TestMediaGetTenLanguagesInFirstSeenOrder(t *testing.T) {
	fake := newMediaFake()
	cfg := mediaFakeConfig(t, fake)
	want := []string{"nl", "en", "fr", "de", "es", "pt", "ja", "ko", "zh", "pl"}
	raw, err := bazarrLanguageProfile(want)
	if err != nil {
		t.Fatal(err)
	}
	var profiles []map[string]any
	if err := json.Unmarshal([]byte(raw), &profiles); err != nil {
		t.Fatal(err)
	}
	items := profiles[0]["items"].([]any)
	duplicate := maps.Clone(items[0].(map[string]any))
	duplicate["id"], duplicate["forced"] = 11, "True"
	profiles[0]["items"] = append([]any{items[0], duplicate}, items[1:]...)
	fake.resources["bazarr/system/languages/profiles"] = profiles
	got, err := GetMediaSettings(cfg.SettingsPath, cfg)
	if err != nil || !slices.Equal(got.Series.Subtitles, want) || !slices.Equal(got.Films.Subtitles, want) {
		t.Fatalf("GET: %+v %v", got, err)
	}
	data, _ := json.Marshal(got)
	if _, err := DecodeMediaSettings(strings.NewReader(string(data))); err != nil {
		t.Fatalf("cannot open settings with ten codes: %v", err)
	}
	if err := ApplyMediaSettings(t.Context(), got, cfg); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(fake.resources["bazarr/system/languages/profiles"][0], profiles[0]) {
		t.Fatal("unchanged selection rewrote variants or their order")
	}
}

func TestGetMediaSettingsUnreadable(t *testing.T) {
	for _, scenario := range []string{"missing config", "unreadable config", "empty key", "invalid code", "too many codes", "invalid JSON", "unauthorized", "forbidden", "5xx", "unreachable"} {
		t.Run(scenario, func(t *testing.T) {
			fake := newMediaFake()
			cfg := mediaHandlerConfig(t, fake)
			want := ""
			switch scenario {
			case "missing config", "unreadable config", "empty key":
				cfg.BazarrAPIKey = ""
				cfg.BazarrConfigPath = filepath.Join(t.TempDir(), "config.yaml")
				if scenario != "missing config" {
					data := "not a config"
					if scenario == "empty key" {
						data = "auth:\n  apikey: ''\n"
					}
					if err := os.WriteFile(cfg.BazarrConfigPath, []byte(data), 0600); err != nil {
						t.Fatal(err)
					}
				}
				_, reason := bazarrAPIKey(cfg.BazarrConfigPath)
				if reason == nil {
					reason = errors.New("config.yaml has no auth.apikey")
				}
				want = "its settings file is missing or can't be opened. Update the media stack on the Apps page."
			case "invalid code":
				fake.resources["bazarr/system/languages/profiles"][0]["items"] = []any{map[string]any{"language": "xx"}}
				want = `Bazarr profile has unsupported language "xx". Fix it in Bazarr under Settings, Languages.`
			case "too many codes":
				codes := []string{"en", "nl", "fr", "de", "es", "it", "pt", "ja", "ko", "zh", "pl"}
				items := []any{}
				for i, code := range codes {
					items = append(items, map[string]any{"id": i + 1, "language": code})
				}
				fake.resources["bazarr/system/languages/profiles"][0]["items"] = items
				want = "profile 1 has more than 10 distinct language codes. Fix it in Bazarr under Settings, Languages."
			case "5xx":
				fake.fail = "GET bazarr/system/settings"
			case "unreachable":
				cfg.HTTPClient.Transport = mediaRoundTripper(func(*http.Request) (*http.Response, error) {
					return nil, errors.New("connection refused")
				})
			case "invalid JSON", "unauthorized", "forbidden":
				response := mediaHandlerConfig(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if scenario == "unauthorized" {
						w.WriteHeader(http.StatusUnauthorized)
					} else if scenario == "forbidden" {
						w.WriteHeader(http.StatusForbidden)
					}
					fmt.Fprint(w, "not JSON")
				}))
				cfg.HTTPClient = response.HTTPClient
				want = "could not read the response from Bazarr. Fix it in Bazarr under Settings, Languages."
				if scenario != "invalid JSON" {
					want = "Bazarr refused the API key. Update the media stack on the Apps page."
				}
			}
			_, err := GetMediaSettings(cfg.SettingsPath, cfg)
			if want == "" {
				if !errors.Is(err, ErrMediaSettingsUnavailable) {
					t.Fatalf("not unavailable: %v", err)
				}
				return
			}
			want = ErrMediaSettingsUnreadable.Error() + ": " + want
			if !errors.Is(err, ErrMediaSettingsUnreadable) || err.Error() != want {
				t.Fatalf("got %v, want %s", err, want)
			}
		})
	}
}

func TestMediaSettingsRequiredSubtitles(t *testing.T) {
	for _, kind := range []string{"Series", "Films"} {
		for _, subtitles := range [][]string{nil, {}} {
			s := DefaultMediaSettings()
			if kind == "Series" {
				s.Series.Subtitles = subtitles
			} else {
				s.Films.Subtitles = subtitles
			}
			want := kind + " needs at least one subtitle language"
			if err := s.Validate(); err == nil || err.Error() != want {
				t.Fatalf("%s: %v", kind, err)
			}
			data, _ := json.Marshal(s)
			if _, err := DecodeMediaSettings(bytes.NewReader(data)); err == nil || err.Error() != want {
				t.Fatalf("decode %s: %v", kind, err)
			}
		}
	}
	for _, count := range []int{0, 10, 11} {
		s := DefaultMediaSettings()
		codes := []string{"en", "nl", "fr", "de", "es", "it", "pt", "ja", "ko", "zh", "pl"}[:count]
		s.Anime.Subtitles, s.Dutch.Subtitles = codes, codes
		if err := s.Validate(); (err == nil) != (count <= 10) {
			t.Fatalf("%d optional languages: %v", count, err)
		}
	}
}

func TestGetMediaSettingsProfileSelection(t *testing.T) {
	for _, scenario := range []string{"empty profile 1", "unrelated invalid code", "unrelated invalid items", "disabled shared", "disabled Films", "disabled preserved Films", "enabled Films", "unused Films"} {
		t.Run(scenario, func(t *testing.T) {
			fake := newMediaFake()
			cfg := mediaHandlerConfig(t, fake)
			wantFilms := []string{"en"}
			switch scenario {
			case "empty profile 1":
				fake.resources["bazarr/system/languages/profiles"][0]["items"] = []any{}
			case "unrelated invalid code":
				fake.resources["bazarr/system/languages/profiles"][1]["items"] = []any{map[string]any{"language": "xx"}}
			case "unrelated invalid items":
				fake.resources["bazarr/system/languages/profiles"][1]["items"] = "not items"
			case "disabled shared":
				fake.bazarrSettings["general"]["movie_default_enabled"] = false
			case "disabled Films", "disabled preserved Films", "enabled Films", "unused Films":
				fake.resources["bazarr/system/languages/profiles"] = append(fake.resources["bazarr/system/languages/profiles"], map[string]any{
					"name": "Films (Iglo-OS)", "profileId": 10, "items": []any{map[string]any{"language": "fr"}},
				})
				if scenario != "unused Films" {
					fake.bazarrSettings["general"]["movie_default_profile"] = 10
					wantFilms = []string{"fr"}
				}
				if scenario == "disabled preserved Films" {
					fake.bazarrSettings["general"]["movie_default_profile"] = 1
				}
				fake.bazarrSettings["general"]["movie_default_enabled"] = scenario == "enabled Films" || scenario == "unused Films"
			}
			got, err := GetMediaSettings(cfg.SettingsPath, cfg)
			if err != nil || !slices.Equal(got.Series.Subtitles, []string{"en"}) || !slices.Equal(got.Films.Subtitles, wantFilms) {
				t.Fatalf("GET: %+v %v", got, err)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("GET returned invalid choices: %v", err)
			}
		})
	}
}
