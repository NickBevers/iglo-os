package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Seerr runs in the same Docker network as Radarr and Sonarr, so it reaches
// them by container name.
const (
	seerrRadarrHost = "radarr"
	seerrSonarrHost = "sonarr"
	// mediaServerJellyfin is MediaServerType.JELLYFIN in Seerr.
	mediaServerJellyfin = 2
)

// The profiles Recyclarr makes, then the defaults of Radarr and Sonarr.
var (
	radarrProfiles = []string{"HD Bluray + WEB", "HD-1080p"}
	sonarrProfiles = []string{"WEB-1080p", "HD-1080p"}
	// animeProfiles are for series that Seerr knows as anime. Without the
	// profile of Recyclarr, anime gets the normal one.
	animeProfiles = []string{"[Anime] Remux-1080p", "WEB-1080p", "HD-1080p"}
)

// Seerr has an anime profile for Sonarr only. For films it has override rules,
// which match a TMDB keyword. Checked against Seerr v3.5.1 on 27 October 2026:
// a rule needs only radarrServiceId, keywords and profileId, and applies to
// every request to that Radarr.
const (
	animeFilmProfile = "[Anime] Remux-1080p"
	// animeKeyword is ANIME_KEYWORD_ID in Seerr, the TMDB keyword "anime".
	animeKeyword = "210024"
)

func newSeerr(baseURL string, timeout time.Duration) *apiClient {
	// Seerr keeps the login in a session cookie.
	jar, _ := cookiejar.New(nil)

	return &apiClient{
		name:    "seerr",
		baseURL: strings.TrimRight(baseURL, "/") + "/api/v1",
		http:    &http.Client{Timeout: timeout, Jar: jar},
	}
}

type seerrService struct {
	ID       int    `json:"id"`
	Hostname string `json:"hostname"`
}

type seerrProfile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// configureSeerr does the setup wizard of Seerr: it logs in with the Jellyfin
// admin, turns on the movie and series libraries, adds Radarr and Sonarr, and
// finishes the setup. Anime gets its own profiles and the anime tags. It does
// nothing when Seerr was already set up.
func configureSeerr(ctx context.Context, cfg Config, seerr *apiClient, tags animeTags) error {
	var public struct {
		Initialized bool `json:"initialized"`
	}
	if err := seerr.do(ctx, http.MethodGet, "/settings/public", nil, &public); err != nil {
		return err
	}
	if public.Initialized {
		cfg.Logf("Seerr is already set up, skipping it")
		return nil
	}

	jellyfinURL := cfg.JellyfinInternalURL
	if jellyfinURL == "" {
		jellyfinURL = cfg.JellyfinURL
	}
	host, port, useSSL, err := splitURL(jellyfinURL)
	if err != nil {
		return err
	}

	login := map[string]any{
		"username":   cfg.JellyfinAdminUsername,
		"password":   cfg.JellyfinAdminPassword,
		"hostname":   host,
		"port":       port,
		"useSsl":     useSSL,
		"urlBase":    "",
		"email":      "",
		"serverType": mediaServerJellyfin,
	}
	if err := seerr.do(ctx, http.MethodPost, "/auth/jellyfin", login, nil); err != nil {
		return fmt.Errorf("log in to Seerr with the Jellyfin admin: %w", err)
	}

	var libraries []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := seerr.do(ctx, http.MethodPost, "/settings/jellyfin/library/sync", nil, &libraries); err != nil {
		return err
	}
	for _, library := range libraries {
		if library.Type != "movie" && library.Type != "show" {
			continue
		}
		path := "/settings/jellyfin/library/" + url.PathEscape(library.ID)
		if err := seerr.do(ctx, http.MethodPut, path, map[string]bool{"enabled": true}, nil); err != nil {
			return err
		}
	}

	movies, series := cfg.folders()
	anime, animeMovies := cfg.animeFolders()
	radarr := map[string]any{
		"name": "Radarr", "hostname": seerrRadarrHost, "port": 7878, "apiKey": cfg.RadarrAPIKey,
		"useSsl": false, "baseUrl": "", "activeDirectory": "/data/media/" + movies,
		"minimumAvailability": "released", "is4k": false, "isDefault": true,
		"tags": []int{}, "syncEnabled": true, "preventSearch": false, "tagRequests": false, "overrideRule": []int{},
	}
	radarrID, radarrFound, err := addSeerrService(ctx, seerr, "radarr", radarr, radarrProfiles, nil)
	if err != nil {
		return err
	}
	// Without the rule, anime films use the normal profile. That is no reason
	// to stop the setup.
	if err := addAnimeFilmRule(ctx, cfg, seerr, radarrID, radarrFound, tags.Radarr, animeMovies, movies); err != nil {
		cfg.Logf("Could not send anime films to their own profile, add an override rule in Seerr: %v", err)
	}

	sonarr := map[string]any{
		"name": "Sonarr", "hostname": seerrSonarrHost, "port": 8989, "apiKey": cfg.SonarrAPIKey,
		"useSsl": false, "baseUrl": "", "activeDirectory": "/data/media/" + series,
		"seriesType": "standard", "animeSeriesType": "anime", "activeAnimeDirectory": "/data/media/" + anime,
		"animeTags":           tagList(tags.Sonarr),
		"enableSeasonFolders": true, "monitorNewItems": "all",
		"is4k": false, "isDefault": true,
		"tags": []int{}, "syncEnabled": true, "preventSearch": false, "tagRequests": false, "overrideRule": []int{},
	}
	if _, _, err := addSeerrService(ctx, seerr, "sonarr", sonarr, sonarrProfiles, animeProfiles); err != nil {
		return err
	}

	// Sonarr numbers seasons as TheTVDB does. With TMDB, Seerr can ask for the
	// wrong season of a split or long anime. Checked against Seerr v3.5.1:
	// the change tests TheTVDB first and fails when it can't be reached.
	metadata := map[string]string{"tv": "tmdb", "anime": "tvdb"}
	if err := seerr.do(ctx, http.MethodPut, "/settings/metadatas", metadata, nil); err != nil {
		cfg.Logf("Could not set TheTVDB for anime in Seerr, set it under Settings > Metadata Providers: %v", err)
	}

	return seerr.do(ctx, http.MethodPost, "/settings/initialize", nil, nil)
}

// addSeerrService adds Radarr or Sonarr, with the first profile that exists,
// and for Sonarr with anime too the first anime profile that exists. It skips
// a service that Seerr already has. It returns the ID of the service in Seerr
// and the profiles it has.
func addSeerrService(ctx context.Context, seerr *apiClient, kind string, settings map[string]any, wanted, anime []string) (int, []seerrProfile, error) {
	var existing []seerrService
	if err := seerr.do(ctx, http.MethodGet, "/settings/"+kind, nil, &existing); err != nil {
		return 0, nil, err
	}
	if index := slices.IndexFunc(existing, func(service seerrService) bool { return service.Hostname == settings["hostname"] }); index >= 0 {
		id := existing[index].ID
		var profiles []seerrProfile
		err := seerr.do(ctx, http.MethodGet, "/settings/"+kind+"/"+strconv.Itoa(id)+"/profiles", nil, &profiles)

		return id, profiles, err
	}

	test := map[string]any{"hostname": settings["hostname"], "port": settings["port"], "apiKey": settings["apiKey"], "useSsl": false, "baseUrl": ""}
	var found struct {
		Profiles []seerrProfile `json:"profiles"`
	}
	if err := seerr.do(ctx, http.MethodPost, "/settings/"+kind+"/test", test, &found); err != nil {
		return 0, nil, err
	}

	profile, ok := pickProfile(found.Profiles, wanted)
	if !ok {
		return 0, nil, fmt.Errorf("%s has no quality profiles", kind)
	}
	settings["activeProfileId"] = profile.ID
	settings["activeProfileName"] = profile.Name
	if anime != nil {
		animeProfile, _ := pickProfile(found.Profiles, anime)
		settings["activeAnimeProfileId"] = animeProfile.ID
		settings["activeAnimeProfileName"] = animeProfile.Name
	}

	var added seerrService
	if err := seerr.do(ctx, http.MethodPost, "/settings/"+kind, settings, &added); err != nil {
		return 0, nil, err
	}

	return added.ID, found.Profiles, nil
}

// addAnimeFilmRule sends films with the anime keyword to the anime profile of
// Radarr, with the anime tag and in the anime movies folder, when Recyclarr
// has made the profile. It adds the rule once.
func addAnimeFilmRule(ctx context.Context, cfg Config, seerr *apiClient, radarrID int, profiles []seerrProfile, tag int, animeMovies, movies string) error {
	index := slices.IndexFunc(profiles, func(profile seerrProfile) bool { return profile.Name == animeFilmProfile })
	if index < 0 {
		cfg.Logf("Radarr has no %s profile yet, so anime films use the normal one. Add an override rule in Seerr once Recyclarr made it", animeFilmProfile)
		return nil
	}

	var rules []struct {
		RadarrServiceID *int   `json:"radarrServiceId"`
		Keywords        string `json:"keywords"`
	}
	if err := seerr.do(ctx, http.MethodGet, "/overrideRule", nil, &rules); err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.RadarrServiceID != nil && *rule.RadarrServiceID == radarrID && slices.Contains(strings.Split(rule.Keywords, ","), animeKeyword) {
			return nil
		}
	}

	rule := map[string]any{"radarrServiceId": radarrID, "keywords": animeKeyword, "profileId": profiles[index].ID}
	if tag > 0 {
		// Seerr keeps the tags of a rule as IDs separated by commas.
		rule["tags"] = strconv.Itoa(tag)
	}
	if animeMovies != movies {
		rule["rootFolder"] = "/data/media/" + animeMovies
	}
	if err := seerr.do(ctx, http.MethodPost, "/overrideRule", rule, nil); err != nil {
		return err
	}
	cfg.Logf("Anime films use the %s profile", animeFilmProfile)

	return nil
}

// tagList is the tags field of a Seerr service: the tag, or none.
func tagList(tag int) []int {
	if tag > 0 {
		return []int{tag}
	}

	return []int{}
}

func pickProfile(profiles []seerrProfile, wanted []string) (seerrProfile, bool) {
	for _, name := range wanted {
		if index := slices.IndexFunc(profiles, func(profile seerrProfile) bool { return profile.Name == name }); index >= 0 {
			return profiles[index], true
		}
	}
	if len(profiles) > 0 {
		return profiles[0], true
	}

	return seerrProfile{}, false
}

// splitURL turns http://192.168.1.20:8096 into the parts Seerr asks for.
func splitURL(raw string) (host string, port int, useSSL bool, err error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", 0, false, fmt.Errorf("invalid Jellyfin URL %q", raw)
	}

	useSSL = parsed.Scheme == "https"
	port = 8096
	if useSSL {
		port = 443
	}
	if parsed.Port() != "" {
		port, _ = strconv.Atoi(parsed.Port())
	}

	return parsed.Hostname(), port, useSSL, nil
}
