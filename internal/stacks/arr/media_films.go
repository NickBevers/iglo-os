package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

const films4KProfile = "4K when available (Iglo-OS)"
const dutch4KProfile = "Dutch 4K (Iglo-OS)"
const films4KBegin = "# Iglo-OS films in 4K begin"
const films4KEnd = "# Iglo-OS films in 4K end"

// Recyclarr v8.8.0 quality-profiles.json, custom-formats.json and
// profile-reference.json, checked against the supplied 2026-11-08 snapshots:
// trash_id supports name, qualities and upgrade overrides. This keeps guide
// CFs and scores without copying them. References by name select this variant.
// ConfiguredCustomFormatProvider loads default groups by this trash_id.
// Golden Rule UHD keeps its x265 (no HDR/DV) penalty. Explicitly score x265
// (HD) at -10000 too, matching HD Bluray + WEB on the live host as reported
// by the owner on 2026-11-08. UHD does not select that format by default.
// The guide's minimum score is 0. Clean 1080p H.264 matches neither x265
// format and passes at 0. Keep the guide penalties on x265 releases.
// HDR scores 500 and HDR10+ Boost 100 from the guide. DV Boost is not selected.
// DV without fallback is explicitly -10000, as in trash-cf-dv-wo-hdr-fallback.
const films4KQualityYAML = `- trash_id: 64fb5f9858489bdac2af690e27c8f42f
  name: "4K when available (Iglo-OS)"
  reset_unmatched_scores:
    enabled: true
  min_format_score: 0
  upgrade:
    allowed: true
    until_quality: Bluray-2160p
    until_score: 10000
  quality_sort: top
  qualities:
    - name: Bluray-2160p
    - name: WEB 2160p
      qualities: [WEBDL-2160p, WEBRip-2160p]
    - name: Bluray-1080p
    - name: WEB 1080p
      qualities: [WEBDL-1080p, WEBRip-1080p]
`

const films4KFormatsYAML = `custom_formats:
  - trash_ids:
      - 493b6d1dbec3c3364c59d7607f7e3405 # HDR
      - caa37d0df9c348912df1fb1d88f9273a # HDR10+ Boost
    assign_scores_to:
      - name: "4K when available (Iglo-OS)"
  - trash_ids:
      - 923b6abef9b17f937fab56cfcf89e1f1 # DV (w/o HDR fallback)
    assign_scores_to:
      - name: "4K when available (Iglo-OS)"
        score: -10000
  - trash_ids:
      - dc98083864ea246d05a42df0d05f81cc # x265 (HD)
    assign_scores_to:
      - name: "4K when available (Iglo-OS)"
        score: -10000
`

// Like the size renderer, preserve every byte outside our marked blocks.
func mediaFilmsYAML(data []byte, enabled bool) ([]byte, error) {
	var base []string
	inside := false
	for _, line := range strings.SplitAfter(string(data), "\n") {
		switch strings.TrimSpace(line) {
		case films4KBegin:
			if inside {
				return nil, errors.New("films in 4K: nested config markers")
			}
			inside = true
		case films4KEnd:
			if !inside {
				return nil, errors.New("films in 4K: unmatched config marker")
			}
			inside = false
		default:
			if !inside {
				base = append(base, line)
			}
		}
	}
	if inside {
		return nil, errors.New("films in 4K: unfinished config markers")
	}
	if !enabled {
		return []byte(strings.Join(base, "")), nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(base, "")), &doc); err != nil {
		return nil, fmt.Errorf("films in 4K: %w", err)
	}
	if len(doc.Content) != 1 {
		return nil, errors.New("films in 4K: missing config")
	}
	instances := yamlMappingValue(doc.Content[0], "radarr")
	if instances == nil || instances.Kind != yaml.MappingNode || len(instances.Content) == 0 {
		return nil, errors.New("films in 4K: missing Radarr instance")
	}
	type insertion struct {
		line  int
		block string
	}
	var inserts []insertion
	block := func(text string, indent int) string {
		prefix := strings.Repeat(" ", indent)
		return prefix + films4KBegin + "\n" + prefix + strings.ReplaceAll(text, "\n", "\n"+prefix) + films4KEnd + "\n"
	}
	for i := 1; i < len(instances.Content); i += 2 {
		instance := instances.Content[i]
		profiles := yamlMappingValue(instance, "quality_profiles")
		if profiles == nil || profiles.Kind != yaml.SequenceNode || profiles.Style == yaml.FlowStyle || len(profiles.Content) == 0 {
			return nil, errors.New("films in 4K: expected block quality_profiles")
		}
		if yamlMappingValue(instance, "custom_formats") != nil {
			return nil, errors.New("films in 4K: unmanaged custom_formats in template")
		}
		inserts = append(inserts, insertion{profiles.Content[0].Line - 1, block(films4KQualityYAML, profiles.Content[0].Column-3)})
		for j := 0; j < len(instance.Content); j += 2 {
			if instance.Content[j].Value == "quality_profiles" {
				key := instance.Content[j]
				inserts = append(inserts, insertion{key.Line - 1, block(films4KFormatsYAML, key.Column-1)})
			}
		}
	}
	slices.SortFunc(inserts, func(a, b insertion) int { return b.line - a.line })
	for _, insert := range inserts {
		base = slices.Insert(base, insert.line, insert.block)
	}
	return []byte(strings.Join(base, "")), nil
}

func filmProfileName(uhd bool) string {
	if uhd {
		return films4KProfile
	}
	return "HD Bluray + WEB"
}

// Sync before copying Dutch or selecting a profile in Seerr. A successful
// size sync alone does not prove that Recyclarr created the requested profile.
func applyFilmConfig(ctx context.Context, enabled bool, cfg MediaSettingsConfig) (rollback func() error, err error) {
	rollback = func() error { return nil }
	before, err := os.ReadFile(cfg.RecyclarrPath)
	if err != nil {
		return rollback, err
	}
	after, err := mediaFilmsYAML(before, enabled)
	if err != nil {
		return rollback, err
	}
	if bytes.Equal(before, after) && !enabled {
		return rollback, nil
	}
	changed := !bytes.Equal(before, after)
	if changed {
		if err := writeFileAtomic(cfg.RecyclarrPath, after, 0o644); err != nil {
			return rollback, err
		}
		rollback = func() error { return writeFileAtomic(cfg.RecyclarrPath, before, 0o644) }
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, rollback())
		}
	}()
	// Disabling needs no new profile before the language steps. The size
	// step runs one sync for both the removed profile config and size changes.
	if !enabled {
		return rollback, nil
	}
	app := newServarr("radarr", cfg.Endpoints.Radarr, "v3", cfg.RadarrAPIKey, cfg.HTTPClient)
	var profiles []seerrProfile
	if err = app.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
		return rollback, err
	}
	missing := !slices.ContainsFunc(profiles, func(p seerrProfile) bool { return p.Name == films4KProfile })
	if changed || missing {
		if cfg.SyncRecyclarr == nil {
			return rollback, errors.New("films in 4K: Recyclarr sync is not configured")
		}
		cfg.Logf("Applying the film quality profile through Recyclarr")
		if err = cfg.SyncRecyclarr(ctx); err != nil {
			return rollback, fmt.Errorf("film profile sync: %w", err)
		}
	}
	if err = app.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
		return rollback, err
	}
	if !slices.ContainsFunc(profiles, func(p seerrProfile) bool { return p.Name == films4KProfile && p.ID > 0 }) {
		return rollback, errors.New("Recyclarr did not create the 4K film profile")
	}
	return rollback, nil
}

// mediaSeerrClient reads credentials without changing any app.
func mediaSeerrClient(cfg MediaSettingsConfig) (*apiClient, error) {
	key := cfg.SeerrAPIKey
	if key == "" {
		path := cfg.SeerrConfigPath
		if path == "" {
			path = "/opt/arr/config/seerr/settings.json"
		}
		var err error
		key, err = mediaSeerrAPIKey(path)
		if err != nil {
			return nil, err
		}
	}
	return &apiClient{name: "seerr", baseURL: strings.TrimRight(cfg.Endpoints.Seerr, "/") + "/api/v1", http: cfg.HTTPClient, headers: map[string]string{"X-API-Key": key}}, nil
}

func seerrFilmService(ctx context.Context, api *apiClient) (map[string]any, error) {
	var services []map[string]any
	if err := api.do(ctx, http.MethodGet, "/settings/radarr", nil, &services); err != nil {
		return nil, err
	}
	index := slices.IndexFunc(services, func(s map[string]any) bool { return s["hostname"] == seerrRadarrHost && s["is4k"] != true })
	if index < 0 {
		return nil, errors.New("Seerr has no normal Radarr service; finish its setup first")
	}
	return services[index], nil
}

// Seerr v3.5.1 routes/settings/radarr.ts, checked 2026-11-08 from the local
// source snapshot: PUT replaces the service. Preserve every other field.
// Keep is4k false: one normal request can use either 1080p or 2160p in Radarr.
func switchSeerrFilms(ctx context.Context, api *apiClient, app *servarr, service map[string]any, enabled bool, previous *seerrProfile) (func() error, error) {
	noop := func() error { return nil }
	var profiles []seerrProfile
	if err := app.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
		return noop, err
	}
	index := -1
	if !enabled && previous != nil {
		index = slices.IndexFunc(profiles, func(p seerrProfile) bool { return p.ID == previous.ID && p.ID > 0 })
	}
	if index < 0 {
		index = slices.IndexFunc(profiles, func(p seerrProfile) bool { return p.Name == filmProfileName(enabled) && p.ID > 0 })
	}
	if index < 0 {
		return noop, fmt.Errorf("Radarr has no %s profile", filmProfileName(enabled))
	}
	before := maps.Clone(service)
	desired := maps.Clone(service)
	profile := profiles[index]
	desired["activeProfileId"], desired["activeProfileName"] = profile.ID, profile.Name
	if sameJSON(before, desired) {
		return noop, nil
	}
	path := "/settings/radarr/" + strconv.Itoa(jsonInt(service["id"]))
	if err := api.do(ctx, http.MethodPut, path, desired, nil); err != nil {
		return noop, err
	}
	return func() error {
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return api.do(restoreCtx, http.MethodPut, path, before, nil)
	}, nil
}

// Seerr v3.5.1 MediaRequest.ts:266-346 stores profileId, type and serverId.
// sub.ts:263-266 uses that profile on approval. Checked 2026-11-08 offline.
// GET /api/v1/request takes take, skip and filter, and answers with
// pageInfo.results as the total (Seerr v3.5.1 server/routes/request.ts:38-60
// and 276-284, checked 2026-11-08). Keep any movie request with a stored
// profile reference, including pending and approved requests, so no status
// enum values are needed.
// Missing pagination or a failed read prevents profile deletion.
func seerrFilmRequestProfiles(ctx context.Context, api *apiClient, services []map[string]any) (map[int]bool, error) {
	used := map[int]bool{}
	for skip := 0; ; {
		var page struct {
			PageInfo struct {
				Total *int `json:"results"`
			} `json:"pageInfo"`
			Results []struct {
				Type      string          `json:"type"`
				ProfileID json.RawMessage `json:"profileId"`
				ServerID  *int            `json:"serverId"`
			} `json:"results"`
		}
		path := fmt.Sprintf("/request?filter=all&take=100&skip=%d", skip)
		if err := api.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		if page.PageInfo.Total == nil || *page.PageInfo.Total < 0 || page.Results == nil {
			return nil, errors.New("Seerr request list has no valid pagination or results")
		}
		for _, request := range page.Results {
			if request.Type != "movie" {
				continue
			}
			if len(request.ProfileID) == 0 {
				return nil, errors.New("Seerr movie request does not expose profileId")
			}
			var profileID *int
			if err := json.Unmarshal(request.ProfileID, &profileID); err != nil {
				return nil, fmt.Errorf("read Seerr request profileId: %w", err)
			}
			if profileID == nil || *profileID <= 0 {
				continue
			}
			// An unset serverId is resolved to a default server on approval.
			// Explicit IDs on another Radarr do not refer to these profiles.
			if request.ServerID != nil && *request.ServerID >= 0 && !slices.ContainsFunc(services, func(s map[string]any) bool {
				return s["hostname"] == seerrRadarrHost && jsonInt(s["id"]) == *request.ServerID
			}) {
				continue
			}
			used[*profileID] = true
		}
		skip += len(page.Results)
		if skip >= *page.PageInfo.Total {
			return used, nil
		}
		if len(page.Results) == 0 {
			return nil, errors.New("Seerr request list ended before its total")
		}
	}
}

// Radarr v6.4.4.10685 QualityProfileService.Delete refuses references from
// movies, import lists and collections. RefreshMovieService.cs:136-147 copies
// a film's profile to a new collection. These service rules come from the
// supplied review, checked 2026-11-08. The local QualityProfileController
// delegates DELETE /qualityprofile/{id} to that service. Never change a
// reference to make deletion possible. One failed deletion must not stop the
// next profile's cleanup.
func cleanFilmProfiles(ctx context.Context, app *servarr, seerr *apiClient, logf func(string, ...any)) error {
	var profiles []seerrProfile
	if err := app.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
		return err
	}
	if !slices.ContainsFunc(profiles, func(p seerrProfile) bool { return p.Name == films4KProfile || p.Name == dutch4KProfile }) {
		return nil
	}
	type profileReference struct {
		QualityProfileID int `json:"qualityProfileId"`
	}
	checks := []struct {
		path, reason string
		references   []profileReference
	}{{path: "/movie", reason: "a film still uses it"}, {path: "/importlist", reason: "an import list still uses it"}, {path: "/collection", reason: "a collection still uses it"}}
	readErrors := make([]error, len(checks)+2)
	var services, rules []map[string]any
	var reads sync.WaitGroup
	for i := range checks {
		reads.Go(func() {
			if err := app.do(ctx, http.MethodGet, checks[i].path, nil, &checks[i].references); err != nil {
				readErrors[i] = fmt.Errorf("read Radarr %s: %w", checks[i].path, err)
			}
		})
	}
	reads.Go(func() { readErrors[len(checks)] = seerr.do(ctx, http.MethodGet, "/settings/radarr", nil, &services) })
	reads.Go(func() { readErrors[len(checks)+1] = seerr.do(ctx, http.MethodGet, "/overrideRule", nil, &rules) })
	reads.Wait()
	requests, err := seerrFilmRequestProfiles(ctx, seerr, services)
	if err != nil {
		readErrors = append(readErrors, fmt.Errorf("read Seerr requests: %w", err))
	}
	readErr := errors.Join(readErrors...)
	failures := []error{readErr}
	for _, profile := range profiles {
		if profile.Name != films4KProfile && profile.Name != dutch4KProfile {
			continue
		}
		reason := ""
		for _, check := range checks {
			if slices.ContainsFunc(check.references, func(r profileReference) bool { return r.QualityProfileID == profile.ID }) {
				reason = check.reason
				break
			}
		}
		if reason == "" && (slices.ContainsFunc(services, func(s map[string]any) bool { return jsonInt(s["activeProfileId"]) == profile.ID }) || slices.ContainsFunc(rules, func(r map[string]any) bool {
			return r["radarrServiceId"] != nil && jsonInt(r["profileId"]) == profile.ID
		})) {
			reason = "Seerr still uses it"
		}
		if reason == "" && requests[profile.ID] {
			reason = "a Seerr movie request still uses it"
		}
		if reason != "" {
			logf("Keeping %s because %s", profile.Name, reason)
			continue
		}
		if readErr != nil {
			logf("Keeping %s because references could not be checked: %v", profile.Name, readErr)
			continue
		}
		if err := app.do(ctx, http.MethodDelete, "/qualityprofile/"+strconv.Itoa(profile.ID), nil, nil); err != nil {
			logf("Keeping %s because it could not be deleted: %v", profile.Name, err)
			failures = append(failures, fmt.Errorf("delete %s: %w", profile.Name, err))
			continue
		}
		logf("Removed unused %s profile", profile.Name)
	}
	return errors.Join(failures...)
}
