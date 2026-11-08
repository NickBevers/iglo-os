package arr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
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
	app := newServarr("radarr", cfg.Endpoints.Radarr, "v3", cfg.RadarrAPIKey, cfg.HTTPClient)
	var profiles []seerrProfile
	if err = app.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
		return rollback, err
	}
	missing := enabled && !slices.ContainsFunc(profiles, func(p seerrProfile) bool { return p.Name == films4KProfile })
	if changed || missing {
		if cfg.SyncRecyclarr == nil {
			return rollback, errors.New("films in 4K: Recyclarr sync is not configured")
		}
		cfg.Logf("Applying the film quality profile through Recyclarr")
		if err = cfg.SyncRecyclarr(ctx); err != nil {
			return rollback, fmt.Errorf("film profile sync: %w", err)
		}
	}
	if enabled {
		if err = app.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
			return rollback, err
		}
		if !slices.ContainsFunc(profiles, func(p seerrProfile) bool { return p.Name == films4KProfile && p.ID > 0 }) {
			return rollback, errors.New("Recyclarr did not create the 4K film profile")
		}
	}
	return rollback, nil
}

// Seerr v3.5.1 routes/settings/radarr.ts, checked 2026-11-08 from the local
// source snapshot: PUT replaces the service. Preserve every other field.
// Keep is4k false: one normal request can use either 1080p or 2160p in Radarr.
func switchSeerrFilms(ctx context.Context, api *apiClient, app *servarr, enabled bool) (func() error, error) {
	noop := func() error { return nil }
	var profiles []seerrProfile
	if err := app.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
		return noop, err
	}
	index := slices.IndexFunc(profiles, func(p seerrProfile) bool { return p.Name == filmProfileName(enabled) && p.ID > 0 })
	if index < 0 {
		return noop, fmt.Errorf("Radarr has no %s profile", filmProfileName(enabled))
	}
	var services []map[string]any
	if err := api.do(ctx, http.MethodGet, "/settings/radarr", nil, &services); err != nil {
		return noop, err
	}
	indexService := slices.IndexFunc(services, func(s map[string]any) bool { return s["hostname"] == seerrRadarrHost && s["is4k"] != true })
	if indexService < 0 {
		return noop, errors.New("Seerr has no normal Radarr service; finish its setup first")
	}
	service := services[indexService]
	before := make(map[string]any, len(service))
	for k, v := range service {
		before[k] = v
	}
	profile := profiles[index]
	service["activeProfileId"], service["activeProfileName"] = profile.ID, profile.Name
	if sameJSON(before, service) {
		return noop, nil
	}
	path := "/settings/radarr/" + strconv.Itoa(jsonInt(service["id"]))
	if err := api.do(ctx, http.MethodPut, path, service, nil); err != nil {
		return noop, err
	}
	return func() error {
		restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return api.do(restoreCtx, http.MethodPut, path, before, nil)
	}, nil
}

// Radarr v6.4.4.10685 QualityProfileController, checked 2026-11-08, exposes
// DELETE /qualityprofile/{id}. Never change a film to make deletion possible.
func cleanFilmProfiles(ctx context.Context, app *servarr, seerr *apiClient, logf func(string, ...any)) error {
	var profiles []seerrProfile
	if err := app.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
		return err
	}
	if !slices.ContainsFunc(profiles, func(p seerrProfile) bool { return p.Name == films4KProfile || p.Name == dutch4KProfile }) {
		return nil
	}
	type movieProfile struct {
		QualityProfileID int `json:"qualityProfileId"`
	}
	var movies []movieProfile
	if err := app.do(ctx, http.MethodGet, "/movie", nil, &movies); err != nil {
		return err
	}
	var services, rules []map[string]any
	if err := seerr.do(ctx, http.MethodGet, "/settings/radarr", nil, &services); err != nil {
		return err
	}
	if err := seerr.do(ctx, http.MethodGet, "/overrideRule", nil, &rules); err != nil {
		return err
	}
	for _, profile := range profiles {
		if profile.Name != films4KProfile && profile.Name != dutch4KProfile {
			continue
		}
		if slices.ContainsFunc(movies, func(m movieProfile) bool { return m.QualityProfileID == profile.ID }) {
			logf("Keeping %s because a film still uses it", profile.Name)
			continue
		}
		used := slices.ContainsFunc(services, func(s map[string]any) bool { return jsonInt(s["activeProfileId"]) == profile.ID }) || slices.ContainsFunc(rules, func(r map[string]any) bool {
			return r["radarrServiceId"] != nil && jsonInt(r["profileId"]) == profile.ID
		})
		if used {
			logf("Keeping %s because Seerr still uses it", profile.Name)
			continue
		}
		if err := app.do(ctx, http.MethodDelete, "/qualityprofile/"+strconv.Itoa(profile.ID), nil, nil); err != nil {
			return err
		}
		logf("Removed unused %s profile", profile.Name)
	}
	return nil
}
