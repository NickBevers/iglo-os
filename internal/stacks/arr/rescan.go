package arr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"iglo/internal/shrink"
)

// Sonarr v4.0.20.3014 and Radarr v6.4.4.10685, checked 2026-11-08:
// DiskScanService.cs:134-171 (Sonarr), :134-170 (Radarr) keeps existing records
// and updates Size. Both UpdateMediaInfoService.cs:80-88 assign only MediaInfo
// and save the same record. Quality, ReleaseGroup, SceneName and Languages
// are not re-derived. Both CustomFormatCalculationService.cs:190-210 (Sonarr),
// :195-216 (Radarr) use stored Languages and Quality, release identity and Size,
// not MediaInfo. Thus the six format kinds in the owner's profiles do not
// read refreshed media info for existing files. LanguageSpecification.cs:49-59
// (Sonarr) and CFLanguageSpecification.cs:49-59 (Radarr) use input.Languages.
// Size changes can affect SizeSpecification, so ShrinkCheck refuses any such
// format with a non-zero profile score. UpgradeDiskSpecification.cs:41-60
// (Sonarr), :40-61 (Radarr) recalculates current formats for upgrade decisions.
// Both UpgradableSpecification.cs:83-113 compare the resulting scores.
// RescanSeriesCommand.cs:7 and RescanMovieCommand.cs:7 take SeriesId/MovieId.
// QualityProfileResource.cs:19,35-39 and CustomFormatResource.cs:17 define the
// scored format list and specifications. See docs/library-shrink.md.

type LibraryPaths struct {
	Paths []string `json:"paths"`
}
type ShrinkCheckResult struct {
	Path      string   `json:"path"`
	Paths     []string `json:"paths"`
	Title     string   `json:"title"`
	App       string   `json:"app,omitempty"`
	ID        int      `json:"id,omitempty"`
	ProfileID int      `json:"profileId,omitempty"`
	State     string   `json:"state"`
	Reason    string   `json:"reason,omitempty"`
}
type RescanResult struct {
	App  string `json:"app"`
	ID   int    `json:"id"`
	Path string `json:"path"`
}
type RescanConfig struct {
	Endpoints                  Endpoints
	SonarrAPIKey, RadarrAPIKey string
	HTTPClient                 *http.Client
}

func ValidateLibraryPaths(paths []string) error {
	if len(paths) == 0 || len(paths) > 100 {
		return shrink.ErrInvalid
	}
	for _, p := range paths {
		if shrink.ValidatePath(p) != nil || !strings.Contains(p, "/") {
			return shrink.ErrInvalid
		}
	}
	return nil
}
func MediaPath(p string) (string, error) {
	if shrink.ValidatePath(p) != nil {
		return "", shrink.ErrInvalid
	}
	return path.Join("/data/media", p), nil
}

type libraryTitle struct {
	ID      int    `json:"id"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Profile int    `json:"qualityProfileId"`
}
type scoredFormat struct {
	Format int `json:"format"`
	Score  int `json:"score"`
}
type shrinkProfile struct {
	ID      int            `json:"id"`
	Formats []scoredFormat `json:"formatItems"`
}
type shrinkFormat struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Specs []struct {
		Implementation string `json:"implementation"`
	} `json:"specifications"`
}

func rescanApps(cfg RescanConfig) map[string]*servarr {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	// Do not forward API keys through an HTTP redirect.
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return map[string]*servarr{"sonarr": newServarr("sonarr", cfg.Endpoints.Sonarr, "v3", cfg.SonarrAPIKey, &copyClient), "radarr": newServarr("radarr", cfg.Endpoints.Radarr, "v3", cfg.RadarrAPIKey, &copyClient)}
}
func lookupLibrary(ctx context.Context, paths []string, apps map[string]*servarr) ([]ShrinkCheckResult, error) {
	if err := ValidateLibraryPaths(paths); err != nil {
		return nil, err
	}
	type listing struct {
		name   string
		titles []libraryTitle
		err    error
	}
	calls := []listing{{name: "sonarr"}, {name: "radarr"}}
	var wg sync.WaitGroup
	for index := range calls {
		call := &calls[index]
		needed := false
		for _, p := range paths {
			film := strings.HasPrefix(p, "movies/") || strings.HasPrefix(p, "anime-movies/")
			if film == (call.name == "radarr") {
				needed = true
			}
		}
		if !needed {
			continue
		}
		wg.Go(func() {
			route := "/series"
			if call.name == "radarr" {
				route = "/movie"
			}
			call.err = apps[call.name].do(ctx, "GET", route, nil, &call.titles)
		})
	}
	wg.Wait()
	lists := map[string][]libraryTitle{}
	for _, call := range calls {
		if call.err != nil {
			return nil, fmt.Errorf("%s library lookup failed", call.name)
		}
		lists[call.name] = call.titles
	}
	results := []ShrinkCheckResult{}
	seen := map[string]int{}
	for _, p := range paths {
		media, _ := MediaPath(p)
		name := "sonarr"
		if strings.HasPrefix(p, "movies/") || strings.HasPrefix(p, "anime-movies/") {
			name = "radarr"
		}
		var best *libraryTitle
		for i := range lists[name] {
			t := &lists[name][i]
			base := strings.TrimRight(t.Path, "/")
			if (media == base || strings.HasPrefix(media, base+"/")) && (best == nil || len(base) > len(best.Path)) {
				best = t
			}
		}
		parts := strings.Split(p, "/")
		unmanagedTitle := strings.Join(parts[:2], "/")
		item := ShrinkCheckResult{Path: unmanagedTitle, Paths: []string{p}, Title: parts[1], State: "ok-unmanaged", Reason: "not managed by Sonarr or Radarr"}
		key := "unmanaged:" + unmanagedTitle
		if best != nil {
			if best.ID <= 0 || best.Profile <= 0 || !strings.HasPrefix(best.Path, "/data/media/") {
				return nil, errors.New("invalid media library record")
			}
			item = ShrinkCheckResult{Path: strings.TrimPrefix(best.Path, "/data/media/"), Paths: []string{p}, Title: best.Title, App: name, ID: best.ID, ProfileID: best.Profile, State: "ok"}
			key = fmt.Sprintf("%s:%d", name, best.ID)
		}
		if i, ok := seen[key]; ok {
			results[i].Paths = append(results[i].Paths, p)
		} else {
			seen[key] = len(results)
			results = append(results, item)
		}
	}
	return results, nil
}
func ShrinkCheck(ctx context.Context, paths []string, cfg RescanConfig) ([]ShrinkCheckResult, error) {
	apps := rescanApps(cfg)
	results, err := lookupLibrary(ctx, paths, apps)
	if err != nil {
		return nil, err
	}
	type formatLookup struct {
		name                  string
		profiles              []shrinkProfile
		formats               []shrinkFormat
		profileErr, formatErr error
	}
	calls := []formatLookup{{name: "sonarr"}, {name: "radarr"}}
	var wg sync.WaitGroup
	for index := range calls {
		call := &calls[index]
		needed := false
		for _, r := range results {
			if r.App == call.name {
				needed = true
			}
		}
		if !needed {
			continue
		}
		wg.Go(func() { call.profileErr = apps[call.name].do(ctx, "GET", "/qualityprofile", nil, &call.profiles) })
		wg.Go(func() { call.formatErr = apps[call.name].do(ctx, "GET", "/customformat", nil, &call.formats) })
	}
	wg.Wait()
	for _, call := range calls {
		name, profiles, formats := call.name, call.profiles, call.formats
		if call.profileErr != nil {
			return nil, fmt.Errorf("%s quality profile lookup failed", name)
		}
		if call.formatErr != nil {
			return nil, fmt.Errorf("%s custom format lookup failed", name)
		}
		for i := range results {
			r := &results[i]
			if r.App != name {
				continue
			}
			var profile *shrinkProfile
			for j := range profiles {
				if profiles[j].ID == r.ProfileID {
					profile = &profiles[j]
				}
			}
			if profile == nil || profile.Formats == nil {
				r.State = "refused"
				r.Reason = "quality profile or its custom formats could not be inspected"
				continue
			}
			for _, scored := range profile.Formats {
				if scored.Score == 0 {
					continue
				}
				var format *shrinkFormat
				for j := range formats {
					if formats[j].ID == scored.Format {
						format = &formats[j]
					}
				}
				if format == nil || len(format.Specs) == 0 {
					r.State = "refused"
					r.Reason = "scored custom format could not be inspected"
					break
				}
				for _, spec := range format.Specs {
					if spec.Implementation == "" {
						r.State = "refused"
						r.Reason = "scored custom format could not be inspected"
						break
					}
					if spec.Implementation == "SizeSpecification" {
						r.State = "refused"
						r.Reason = "quality profile scores a custom format with SizeSpecification: " + format.Name
						break
					}
				}
				if r.State == "refused" {
					break
				}
			}
		}
	}
	return results, nil
}
func Rescan(ctx context.Context, paths []string, cfg RescanConfig) ([]RescanResult, error) {
	apps := rescanApps(cfg)
	titles, err := lookupLibrary(ctx, paths, apps)
	if err != nil {
		return nil, err
	}
	results := []RescanResult{}
	for _, title := range titles {
		if title.App == "" {
			continue
		}
		body := map[string]any{"name": "RescanSeries", "seriesId": title.ID}
		if title.App == "radarr" {
			body = map[string]any{"name": "RescanMovie", "movieId": title.ID}
		}
		if err := apps[title.App].do(ctx, "POST", "/command", body, nil); err != nil {
			return nil, fmt.Errorf("%s rescan failed", title.App)
		}
		results = append(results, RescanResult{App: title.App, ID: title.ID, Path: title.Path})
	}
	return results, nil
}
