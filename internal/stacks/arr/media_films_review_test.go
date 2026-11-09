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
)

func filmCleanupFixture(t *testing.T) (*mediaFake, MediaSettingsConfig, *[]string, int) {
	t.Helper()
	f := newMediaFake()
	cfg, _ := filmFakeConfig(t, f)
	settings := DefaultMediaSettings()
	settings.UHDFilms = true
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	dutchID := 0
	for _, p := range f.resources["radarr/qualityprofile"] {
		if p["name"] == dutch4KProfile {
			dutchID = jsonInt(p["id"])
		}
	}
	if dutchID == 0 {
		t.Fatal("Dutch 4K profile missing")
	}
	f.resources["seerr/settings/radarr"][0]["activeProfileId"] = 3
	for _, rule := range f.resources["seerr/overrideRule"] {
		if rule["radarrServiceId"] != nil && (jsonInt(rule["profileId"]) == 80 || jsonInt(rule["profileId"]) == dutchID) {
			rule["profileId"] = 3
		}
	}
	logs := new([]string)
	cfg.Logf = func(format string, args ...any) { *logs = append(*logs, fmt.Sprintf(format, args...)) }
	return f, cfg, logs, dutchID
}

func runFilmCleanup(t *testing.T, cfg MediaSettingsConfig) error {
	t.Helper()
	seerr, err := mediaSeerrClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := newServarr("radarr", cfg.Endpoints.Radarr, "v3", cfg.RadarrAPIKey, cfg.HTTPClient)
	return cleanFilmProfiles(t.Context(), app, seerr, cfg.Logf)
}

func hasFilmProfile(f *mediaFake, id int) bool {
	return slices.ContainsFunc(f.resources["radarr/qualityprofile"], func(p map[string]any) bool { return jsonInt(p["id"]) == id })
}

func TestFilmCleanupRadarrReferences(t *testing.T) {
	for _, resource := range []string{"collection", "importlist"} {
		for _, target := range []string{films4KProfile, dutch4KProfile} {
			t.Run(resource+"/"+target, func(t *testing.T) {
				f, cfg, logs, dutchID := filmCleanupFixture(t)
				keep, remove := 80, dutchID
				if target == dutch4KProfile {
					keep, remove = dutchID, 80
				}
				f.resources["radarr/"+resource] = []map[string]any{{"qualityProfileId": keep}}
				if err := runFilmCleanup(t, cfg); err != nil {
					t.Fatal(err)
				}
				if !hasFilmProfile(f, keep) || hasFilmProfile(f, remove) {
					t.Fatalf("profile reference cleanup: %v", f.resources["radarr/qualityprofile"])
				}
				reason := "a collection still uses it"
				if resource == "importlist" {
					reason = "an import list still uses it"
				}
				if !strings.Contains(strings.Join(*logs, "\n"), "Keeping "+target+" because "+reason) {
					t.Fatalf("missing reason: %v", *logs)
				}
			})
		}
	}
}

func TestFilmCleanupContinuesAfterDeleteError(t *testing.T) {
	f, cfg, logs, dutchID := filmCleanupFixture(t)
	f.fail = "DELETE radarr/qualityprofile/80"
	if err := runFilmCleanup(t, cfg); err == nil {
		t.Fatal("expected delete failure")
	}
	if !hasFilmProfile(f, 80) || hasFilmProfile(f, dutchID) {
		t.Fatal("first delete failure stopped the other profile's cleanup")
	}
	if !strings.Contains(strings.Join(*logs, "\n"), "Keeping "+films4KProfile+" because it could not be deleted") {
		t.Fatalf("missing delete reason: %v", *logs)
	}
}

func TestFilmCleanupReadErrorsKeepEachProfile(t *testing.T) {
	for _, path := range []string{"radarr/movie", "radarr/importlist", "radarr/collection", "seerr/request"} {
		t.Run(path, func(t *testing.T) {
			f, cfg, logs, dutchID := filmCleanupFixture(t)
			f.fail = "GET " + path
			if err := runFilmCleanup(t, cfg); err == nil {
				t.Fatal("expected read failure")
			}
			if !hasFilmProfile(f, 80) || !hasFilmProfile(f, dutchID) {
				t.Fatal("deleted a profile with unreadable references")
			}
			for _, name := range []string{films4KProfile, dutch4KProfile} {
				if !strings.Contains(strings.Join(*logs, "\n"), "Keeping "+name+" because references could not be checked") {
					t.Fatalf("missing per-profile read reason: %v", *logs)
				}
			}
		})
	}
}

func TestFilmCleanupSeerrRequests(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		server     *int
		wantKeep   bool
		status     int
		noProfile  bool
	}{
		{name: "pending", kind: "movie", wantKeep: true, status: 1},
		{name: "approved", kind: "movie", wantKeep: true, status: 2},
		{name: "default server sentinel", kind: "movie", server: new(-1), wantKeep: true, status: 1},
		{name: "explicit server", kind: "movie", server: new(0), wantKeep: true, status: 1},
		{name: "another server", kind: "movie", server: new(9), status: 1},
		{name: "television", kind: "tv", status: 1},
		{name: "no profile override", kind: "movie", noProfile: true, status: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, cfg, logs, dutchID := filmCleanupFixture(t)
			// Put the reference beyond the first capped page. Status numbers
			// are deliberately ignored by the conservative all-status check.
			f.requestPageSize = 2
			f.resources["seerr/request"] = []map[string]any{
				{"id": 1, "type": "movie", "profileId": 3},
				{"id": 2, "type": "tv", "profileId": 80},
				{"id": 3, "type": "movie", "profileId": 3},
				{"id": 4, "type": "movie", "profileId": 3},
				{"id": 5, "type": test.kind, "profileId": 80, "serverId": test.server, "status": test.status},
			}
			if test.noProfile {
				f.resources["seerr/request"][4]["profileId"] = nil
			}
			if err := runFilmCleanup(t, cfg); err != nil {
				t.Fatal(err)
			}
			if hasFilmProfile(f, 80) != test.wantKeep || hasFilmProfile(f, dutchID) {
				t.Fatal("request reference cleanup chose the wrong profiles")
			}
			for _, skip := range []int{0, 2, 4} {
				if !slices.Contains(f.reads, fmt.Sprintf("seerr/request?filter=all&take=100&skip=%d", skip)) {
					t.Fatalf("missed request page %d: %v", skip, f.reads)
				}
			}
			if test.wantKeep && !strings.Contains(strings.Join(*logs, "\n"), "a Seerr movie request still uses it") {
				t.Fatalf("missing request reason: %v", *logs)
			}
		})
	}
}

func TestFilmCleanupUnverifiedRequestResponse(t *testing.T) {
	for _, response := range []string{`{"results":[]}`, `{"pageInfo":{"results":2},"results":[]}`, `{"pageInfo":{"results":1},"results":[{"type":"movie","id":1}]}`, `{"pageInfo":{"results":1},"results":[{"type":"movie","profileId":"bad"}]}`} {
		t.Run(response, func(t *testing.T) {
			f, cfg, logs, dutchID := filmCleanupFixture(t)
			clientCfg := mediaHandlerConfig(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/seerr/api/v1/request" {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, response)
					return
				}
				f.ServeHTTP(w, r)
			}))
			cfg.HTTPClient = clientCfg.HTTPClient
			if err := runFilmCleanup(t, cfg); err == nil {
				t.Fatal("expected incomplete request read")
			}
			if !hasFilmProfile(f, 80) || !hasFilmProfile(f, dutchID) {
				t.Fatal("deleted profile after an incomplete request read")
			}
			if !strings.Contains(strings.Join(*logs, "\n"), "Seerr requests") {
				t.Fatalf("missing request error: %v", *logs)
			}
		})
	}
}

func TestFilmSwitchPreflightBeforeWrites(t *testing.T) {
	for _, previous := range []bool{false, true} {
		for _, failure := range []string{"missing key", "bad key", "missing service", "offline"} {
			t.Run(fmt.Sprintf("%v/%s", previous, failure), func(t *testing.T) {
				f := newMediaFake()
				cfg, syncs := filmFakeConfig(t, f)
				settings := DefaultMediaSettings()
				settings.UHDFilms = previous
				if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
					t.Fatal(err)
				}
				switch failure {
				case "missing key":
					cfg.SeerrAPIKey = ""
					cfg.SeerrConfigPath = filepath.Join(t.TempDir(), "missing.json")
				case "bad key":
					cfg.SeerrAPIKey = "invalid"
				case "missing service":
					f.resources["seerr/settings/radarr"] = []map[string]any{}
				case "offline":
					f.fail = "GET seerr/settings/radarr"
				}
				beforeApps, _ := json.Marshal(f.resources)
				beforeBazarr, _ := json.Marshal(f.bazarrSettings)
				beforeYAML, _ := os.ReadFile(cfg.RecyclarrPath)
				beforeSettings, _ := os.ReadFile(cfg.SettingsPath)
				writes, syncCount := len(f.writes), *syncs
				settings.UHDFilms = !previous
				if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil {
					t.Fatal("expected preflight failure")
				}
				afterApps, _ := json.Marshal(f.resources)
				afterBazarr, _ := json.Marshal(f.bazarrSettings)
				afterYAML, _ := os.ReadFile(cfg.RecyclarrPath)
				afterSettings, _ := os.ReadFile(cfg.SettingsPath)
				if len(f.writes) != writes || *syncs != syncCount || !bytes.Equal(beforeApps, afterApps) || !bytes.Equal(beforeBazarr, afterBazarr) || !bytes.Equal(beforeYAML, afterYAML) || !bytes.Equal(beforeSettings, afterSettings) {
					t.Fatal("failed preflight changed apps, YAML or saved settings")
				}
			})
		}
	}
}

func TestFilmSaveWhileOnDoesNotRequireSeerr(t *testing.T) {
	for _, failure := range []string{"offline", "missing key"} {
		t.Run(failure, func(t *testing.T) {
			f := newMediaFake()
			cfg, _ := filmFakeConfig(t, f)
			settings := DefaultMediaSettings()
			settings.UHDFilms = true
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			if failure == "offline" {
				f.fail = "GET seerr/settings/radarr"
			} else {
				cfg.SeerrAPIKey = ""
				cfg.SeerrConfigPath = filepath.Join(t.TempDir(), "missing.json")
			}
			settings.Films.Subtitles = []string{"nl"}
			writes := len(f.writes)
			if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
				t.Fatal(err)
			}
			if slices.Contains(f.writes[writes:], "PUT seerr/settings/radarr/0") {
				t.Fatal("ordinary save switched Seerr's default")
			}
			got, err := GetMediaSettings(cfg.SettingsPath)
			if err != nil || !sameJSON(got, settings) {
				t.Fatalf("settings were not saved: %+v %v", got, err)
			}
		})
	}
}

func TestFilmPreviousSeerrProfileAndSingleOffSync(t *testing.T) {
	for _, exists := range []bool{false, true} {
		for _, resize := range []bool{false, true} {
			t.Run(fmt.Sprintf("exists=%v/resize=%v", exists, resize), func(t *testing.T) {
				f := newMediaFake()
				cfg, syncs := filmFakeConfig(t, f)
				raw, _ := json.Marshal(f.resources["radarr/qualityprofile"][0])
				var custom map[string]any
				json.Unmarshal(raw, &custom)
				custom["id"], custom["name"] = 7, "My films"
				f.resources["radarr/qualityprofile"] = append(f.resources["radarr/qualityprofile"], custom)
				service := f.resources["seerr/settings/radarr"][0]
				service["activeProfileId"], service["activeProfileName"] = 7, "My films"
				settings := DefaultMediaSettings()
				settings.UHDFilms = true
				if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
					t.Fatal(err)
				}
				saved, err := readMediaSettings(cfg.SettingsPath)
				if err != nil || saved.PreviousFilmProfile == nil || saved.PreviousFilmProfile.ID != 7 || saved.PreviousFilmProfile.Name != "My films" {
					t.Fatalf("previous profile was not saved: %+v %v", saved, err)
				}
				// Another save and a manual default change must not replace the
				// choice captured when the switch was first turned on.
				f.resources["seerr/settings/radarr"][0]["activeProfileId"] = 3
				f.resources["seerr/settings/radarr"][0]["activeProfileName"] = "HD Bluray + WEB"
				settings.Films.Subtitles = []string{"nl"}
				writes := len(f.writes)
				if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
					t.Fatal(err)
				}
				if slices.Contains(f.writes[writes:], "PUT seerr/settings/radarr/0") {
					t.Fatal("ordinary save replaced a manual Seerr default")
				}
				if !exists {
					f.resources["radarr/qualityprofile"] = slices.DeleteFunc(f.resources["radarr/qualityprofile"], func(p map[string]any) bool { return jsonInt(p["id"]) == 7 })
				}
				settings.UHDFilms = false
				if resize {
					settings.Sizes = SizeLimits{Films: 33, Films4K: 208}
				}
				syncCount := *syncs
				if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
					t.Fatal(err)
				}
				if *syncs != syncCount+1 {
					t.Fatalf("switch off ran %d syncs, want 1", *syncs-syncCount)
				}
				wantID, wantName := 3, "HD Bluray + WEB"
				if exists {
					wantID, wantName = 7, "My films"
				}
				service = f.resources["seerr/settings/radarr"][0]
				if jsonInt(service["activeProfileId"]) != wantID || service["activeProfileName"] != wantName {
					t.Fatalf("restored wrong default: %v", service)
				}
				saved, err = readMediaSettings(cfg.SettingsPath)
				if err != nil || saved.PreviousFilmProfile != nil {
					t.Fatalf("previous choice was not cleared after off: %+v %v", saved, err)
				}
			})
		}
	}
}

func TestFilmOffSyncFailureRestoresState(t *testing.T) {
	f := newMediaFake()
	cfg, _ := filmFakeConfig(t, f)
	settings := DefaultMediaSettings()
	settings.UHDFilms = true
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	beforeYAML, _ := os.ReadFile(cfg.RecyclarrPath)
	beforeSaved, _ := os.ReadFile(cfg.SettingsPath)
	beforeService, _ := json.Marshal(f.resources["seerr/settings/radarr"])
	beforeRules, _ := json.Marshal(f.resources["seerr/overrideRule"])
	syncs := 0
	cfg.SyncRecyclarr = func(context.Context) error { syncs++; return errors.New("offline") }
	settings.UHDFilms = false
	settings.Sizes.Films4K = 208
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err == nil {
		t.Fatal("expected failed off sync")
	}
	afterYAML, _ := os.ReadFile(cfg.RecyclarrPath)
	afterSaved, _ := os.ReadFile(cfg.SettingsPath)
	afterService, _ := json.Marshal(f.resources["seerr/settings/radarr"])
	afterRules, _ := json.Marshal(f.resources["seerr/overrideRule"])
	if syncs != 1 || !bytes.Equal(beforeYAML, afterYAML) || !bytes.Equal(beforeSaved, afterSaved) || !bytes.Equal(beforeService, afterService) || !bytes.Equal(beforeRules, afterRules) {
		t.Fatal("failed single off sync did not restore YAML, saved choice, default and rule")
	}
}

func TestFilmOffWithoutPreviousProfileMetadata(t *testing.T) {
	f := newMediaFake()
	cfg, _ := filmFakeConfig(t, f)
	settings := DefaultMediaSettings()
	settings.UHDFilms = true
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := readMediaSettings(cfg.SettingsPath)
	if err != nil {
		t.Fatal(err)
	}
	saved.PreviousFilmProfile = nil
	data, _ := json.Marshal(saved)
	if err := os.WriteFile(cfg.SettingsPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	settings.UHDFilms = false
	if err := ApplyMediaSettings(t.Context(), settings, cfg); err != nil {
		t.Fatal(err)
	}
	if jsonInt(f.resources["seerr/settings/radarr"][0]["activeProfileId"]) != 3 {
		t.Fatal("old 4K settings did not fall back to HD")
	}
}
