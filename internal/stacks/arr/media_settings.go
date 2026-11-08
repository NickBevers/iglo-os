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
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MediaSettings are the languages, film quality and download size limits.
type MediaSettings struct {
	Anime    KindSettings `json:"anime"`
	Dutch    KindSettings `json:"dutch"`
	Series   KindSettings `json:"series"`
	Films    KindSettings `json:"films"`
	Sizes    SizeLimits   `json:"sizes"`
	UHDFilms bool         `json:"uhdFilms"`
}

// SizeLimits stores integer MB per minute. Zero keeps the guide's sizes.
// Sonarr shares sizes.series across series, Dutch series and anime. Radarr
// uses sizes.films for HD and lower, and sizes.films4k for 2160p qualities.
// Missing fields in older files read as zero.
type SizeLimits struct {
	Series  int `json:"series"`
	Films   int `json:"films"`
	Films4K int `json:"films4k"`
}

func (s SizeLimits) Validate() error {
	for _, limit := range []struct {
		name  string
		value int
		max   int
	}{{"Series", s.Series, 400}, {"Films", s.Films, 400}, {"Films in 4K", s.Films4K, 1000}} {
		if limit.value != 0 && (limit.value < 10 || limit.value > limit.max) {
			return fmt.Errorf("%s: maximum download size must be 0 or between 10 and %d MB per minute", limit.name, limit.max)
		}
	}
	return nil
}

// KindSettings are the languages of one kind.
type KindSettings struct {
	// Subtitles are the subtitle languages Bazarr downloads, as ISO 639-1
	// codes like "en" or "nl". Only Anime and Dutch can be empty,
	// which means Bazarr leaves that kind alone.
	Subtitles []string `json:"subtitles"`
	// Audio is "original" to refuse releases without the original language,
	// "dutch" to want a Dutch track including dubs, or "any". Validation
	// currently refuses "any" because Recyclarr would undo it.
	Audio string `json:"audio"`
}

// DefaultMediaSettings keeps bazarrForm's anime exclusions and English series
// and film subtitles.
// Recyclarr's normal and anime profiles want original audio. Dutch content
// uses Dutch audio and no extra subtitles in the per-kind contract.
func DefaultMediaSettings() MediaSettings {
	return MediaSettings{
		Anime:  KindSettings{Subtitles: []string{}, Audio: "original"},
		Dutch:  KindSettings{Subtitles: []string{}, Audio: "dutch"},
		Series: KindSettings{Subtitles: []string{"en"}, Audio: "original"},
		Films:  KindSettings{Subtitles: []string{"en"}, Audio: "original"},
	}
}

func (s MediaSettings) kinds() []struct {
	name     string
	settings KindSettings
} {
	return []struct {
		name     string
		settings KindSettings
	}{
		{"Anime", s.Anime}, {"Dutch", s.Dutch}, {"Series", s.Series}, {"Films", s.Films},
	}
}

func (s MediaSettings) Validate() error {
	if err := s.Sizes.Validate(); err != nil {
		return err
	}
	for _, kind := range s.kinds() {
		if (kind.name == "Series" || kind.name == "Films") && len(kind.settings.Subtitles) == 0 {
			return fmt.Errorf("%s needs at least one subtitle language", kind.name)
		}
		if len(kind.settings.Subtitles) > 10 {
			return fmt.Errorf("%s: at most 10 subtitle languages", kind.name)
		}
		seen := map[string]bool{}
		for _, language := range kind.settings.Subtitles {
			if !validBazarrLanguage(language) {
				return fmt.Errorf("%s: unsupported subtitle language %q", kind.name, language)
			}
			if seen[language] {
				return fmt.Errorf("%s: duplicate subtitle language %q", kind.name, language)
			}
			seen[language] = true
		}
		want := "original"
		if kind.name == "Dutch" {
			want = "dutch"
		}
		// Recyclarr v8.8.0, checked 7 November 2026: UpdatedFormatScore and
		// QualityProfileTransactionPhase overwrite managed scores every sync.
		// Saving the old score cannot make "any" survive the daily sync. Keep
		// original audio until Recyclarr's config can express this choice too.
		if kind.settings.Audio == "any" && (kind.name == "Series" || kind.name == "Films") {
			return fmt.Errorf("%s: audio any is not supported while Recyclarr manages language scores; use original", kind.name)
		}
		if kind.settings.Audio != want {
			return fmt.Errorf("%s: audio must be %q", kind.name, want)
		}
	}
	return nil
}

// DecodeMediaSettings refuses unknown fields, trailing values and large input
// before any app is changed. All four kinds must have a valid audio choice.
func DecodeMediaSettings(r io.Reader) (MediaSettings, error) {
	const limit = 64 * 1024
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return MediaSettings{}, err
	}
	if len(data) > limit {
		return MediaSettings{}, errors.New("media settings exceed 64 KiB")
	}
	var settings MediaSettings
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return settings, fmt.Errorf("media settings JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return settings, errors.New("media settings must contain exactly one JSON object")
	}
	settings = settings.normalized()
	return settings, settings.Validate()
}

func (s MediaSettings) normalized() MediaSettings {
	for _, kind := range []*KindSettings{&s.Anime, &s.Dutch, &s.Series, &s.Films} {
		if kind.Subtitles == nil {
			kind.Subtitles = []string{}
		}
	}
	return s
}

// ErrMediaSettingsUnavailable asks the user to retry once Bazarr has started.
var ErrMediaSettingsUnavailable = errors.New("Bazarr is not answering yet. Try again in a minute.")

// ErrMediaSettingsUnreadable means Bazarr needs a settings fix or stack update.
var ErrMediaSettingsUnreadable = errors.New("Iglo-OS can't read Bazarr's settings")

func mediaSettingsUnreadable(reason string) error {
	return fmt.Errorf("%w: %s. Fix it in Bazarr under Settings, Languages.", ErrMediaSettingsUnreadable, reason)
}

func mediaSettingsUpdate(reason string) error {
	return fmt.Errorf("%w: %s. Update the media stack on the Apps page.", ErrMediaSettingsUnreadable, reason)
}

const MediaSettingsPath = "/opt/arr/iglo-media.json"
const dutchProfile = "Dutch (Iglo-OS)"
const dutchAudioFormat = "Dutch audio (Iglo-OS)"
const notDutchAudioFormat = "Not Dutch audio (Iglo-OS)"
const notOriginalFormat = "Language: Not Original"

type savedMediaSettings struct {
	Version             int            `json:"version"`
	Settings            MediaSettings  `json:"settings"`
	SeerrRuleIDs        map[string]int `json:"seerrRuleIds,omitempty"`
	PreviousFilmProfile *seerrProfile  `json:"previousFilmProfile,omitempty"`
}

func readMediaSettings(path string) (savedMediaSettings, error) {
	if path == "" {
		path = MediaSettingsPath
	}
	saved := savedMediaSettings{Version: 1, Settings: DefaultMediaSettings()}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return saved, nil
	}
	if err != nil {
		return saved, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	// Do not let incomplete files inherit defaults and appear applied.
	saved = savedMediaSettings{}
	if err := decoder.Decode(&saved); err != nil {
		return saved, fmt.Errorf("read media settings: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return saved, errors.New("media settings file has trailing data")
	}
	if saved.Version != 1 {
		return saved, fmt.Errorf("unsupported media settings version %d", saved.Version)
	}
	saved.Settings = saved.Settings.normalized()
	return saved, saved.Settings.Validate()
}

// GetMediaSettings returns saved choices, or reads Bazarr on an older stack.
// Without saved choices, Bazarr must answer before choices can be shown.
func GetMediaSettings(path string, configs ...MediaSettingsConfig) (MediaSettings, error) {
	if path == "" {
		path = MediaSettingsPath
	}
	saved, err := readMediaSettings(path)
	if err != nil {
		return saved.Settings, err
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return saved.Settings, err
	}
	cfg := MediaSettingsConfig{}
	if len(configs) > 0 {
		cfg = configs[0]
	}
	if cfg.EnvPath == "" {
		cfg.EnvPath = filepath.Join(filepath.Dir(path), ".env")
	}
	settings := DefaultMediaSettings()
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Endpoints.Bazarr == "" {
		cfg.Endpoints.Bazarr = DefaultEndpoints.Bazarr
	}
	if cfg.BazarrAPIKey == "" {
		if cfg.BazarrConfigPath == "" {
			_, cfg.BazarrConfigPath, _ = MediaSettingsPaths(cfg.EnvPath)
		}
		cfg.BazarrAPIKey, err = bazarrAPIKey(cfg.BazarrConfigPath)
		if err != nil || cfg.BazarrAPIKey == "" {
			if err == nil {
				err = errors.New("config.yaml has no auth.apikey")
			}
			return MediaSettings{}, mediaSettingsUpdate("its settings file is missing or can't be opened")
		}
	}
	api := &apiClient{name: "bazarr", baseURL: strings.TrimRight(cfg.Endpoints.Bazarr, "/") + "/api", headers: map[string]string{"X-API-KEY": cfg.BazarrAPIKey}, http: cfg.HTTPClient}
	var profiles []map[string]any
	var current map[string]map[string]any
	var reads sync.WaitGroup
	var readErrors [2]error
	reads.Go(func() {
		readErrors[0] = api.do(context.Background(), http.MethodGet, "/system/languages/profiles", nil, &profiles)
	})
	reads.Go(func() {
		readErrors[1] = api.do(context.Background(), http.MethodGet, "/system/settings", nil, &current)
	})
	reads.Wait()
	for _, err := range readErrors {
		if err == nil {
			continue
		}
		var transport *url.Error
		var status *statusError
		if errors.As(err, &transport) || (errors.As(err, &status) && status.status >= 500) {
			return MediaSettings{}, ErrMediaSettingsUnavailable
		}
		if errors.As(err, &status) && (status.status == http.StatusUnauthorized || status.status == http.StatusForbidden) {
			return MediaSettings{}, mediaSettingsUpdate("Bazarr refused the API key")
		}
		return MediaSettings{}, mediaSettingsUnreadable("could not read the response from Bazarr")
	}
	for _, profile := range profiles {
		if jsonInt(profile["profileId"]) != 1 {
			continue
		}
		languages, err := mediaProfileLanguages(profile)
		if err != nil {
			return MediaSettings{}, mediaSettingsUnreadable(err.Error())
		}
		if len(languages) > 10 {
			return MediaSettings{}, mediaSettingsUnreadable("profile 1 has more than 10 distinct language codes")
		}
		if len(languages) > 0 {
			settings.Series.Subtitles = languages
			settings.Films.Subtitles = slices.Clone(languages)
		}
	}
	// Older None settings disabled the default but kept films on their profile.
	for _, profile := range profiles {
		if profile["name"] != "Films (Iglo-OS)" {
			continue
		}
		if current["general"]["movie_default_enabled"] != false && fmt.Sprint(current["general"]["movie_default_profile"]) != fmt.Sprint(profile["profileId"]) {
			continue
		}
		languages, err := mediaProfileLanguages(profile)
		if err != nil {
			return MediaSettings{}, mediaSettingsUnreadable(err.Error())
		}
		if len(languages) > 10 {
			return MediaSettings{}, mediaSettingsUnreadable("Films (Iglo-OS) has more than 10 distinct language codes")
		}
		if len(languages) > 0 {
			settings.Films.Subtitles = languages
		}
	}
	for _, kind := range []struct {
		name     string
		settings *KindSettings
		excluded bool
	}{
		{"Anime", &settings.Anime, mediaContains(current["sonarr"]["excluded_series_types"], "anime")},
		{"Dutch", &settings.Dutch, mediaContains(current["sonarr"]["excluded_tags"], "dutch") || mediaContains(current["radarr"]["excluded_tags"], "dutch")},
	} {
		if kind.excluded {
			continue
		}
		for _, profile := range profiles {
			if profile["name"] == kind.name+" (Iglo-OS)" {
				languages, err := mediaProfileLanguages(profile)
				if err != nil {
					return MediaSettings{}, mediaSettingsUnreadable(err.Error())
				}
				kind.settings.Subtitles = languages
			}
		}
	}
	return settings.normalized(), nil
}

func mediaContains(raw any, value string) bool {
	values, _ := raw.([]any)
	return slices.Contains(values, any(value))
}

// Bazarr v1.6.2, checked 7 November 2026: get_profiles_list returns items
// as objects with language and id. Downloads use a set, and cutoff uses IDs.
// Item order does not choose a preferred language (subtitles/download.py).
func mediaProfileLanguages(profile map[string]any) ([]string, error) {
	languages := []string{}
	items, ok := profile["items"].([]any)
	if !ok {
		return nil, errors.New("Bazarr profile has invalid items")
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("Bazarr profile has an invalid language item")
		}
		code, _ := item["language"].(string)
		if !validBazarrLanguage(code) {
			return nil, fmt.Errorf("Bazarr profile has unsupported language %q", code)
		}
		if !slices.Contains(languages, code) {
			languages = append(languages, code)
		}
	}
	return languages, nil
}

// MediaSettingsConfig supplies the same local apps and credentials as configure.
// Keys may be supplied directly. Otherwise Bazarr and Seerr read their config
// under the stack folder. Empty paths and endpoints use the installed defaults.
type MediaSettingsConfig struct {
	Endpoints        Endpoints
	SonarrAPIKey     string
	RadarrAPIKey     string
	BazarrAPIKey     string
	SeerrAPIKey      string
	BazarrConfigPath string
	SeerrConfigPath  string
	EnvPath          string
	SettingsPath     string
	RecyclarrPath    string
	SyncRecyclarr    func(context.Context) error
	HTTPClient       *http.Client
	Logf             func(string, ...any)
}

// ApplyMediaSettings reconciles the media settings owned by Iglo-OS.
// It applies sizes last, then saves choices. Dutch rule failures only warn.
// Changing the default film profile requires Seerr to accept the change.
func ApplyMediaSettings(ctx context.Context, settings MediaSettings, cfg MediaSettingsConfig) (err error) {
	settings = settings.normalized()
	if err := settings.Validate(); err != nil {
		return err
	}
	if cfg.SettingsPath == "" {
		cfg.SettingsPath = MediaSettingsPath
	}
	saved, err := readMediaSettings(cfg.SettingsPath)
	if err != nil {
		return err
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	for _, endpoint := range []struct {
		value    *string
		fallback string
	}{
		{&cfg.Endpoints.Sonarr, DefaultEndpoints.Sonarr}, {&cfg.Endpoints.Radarr, DefaultEndpoints.Radarr},
		{&cfg.Endpoints.Bazarr, DefaultEndpoints.Bazarr}, {&cfg.Endpoints.Seerr, DefaultEndpoints.Seerr},
	} {
		if *endpoint.value == "" {
			*endpoint.value = endpoint.fallback
		}
	}
	if cfg.SonarrAPIKey == "" || cfg.RadarrAPIKey == "" {
		return errors.New("Sonarr and Radarr API keys are required")
	}
	if cfg.BazarrAPIKey == "" {
		if cfg.BazarrConfigPath == "" {
			cfg.BazarrConfigPath = "/opt/arr/config/bazarr/config/config.yaml"
		}
		key, err := bazarrAPIKey(cfg.BazarrConfigPath)
		if err != nil {
			return err
		}
		cfg.BazarrAPIKey = key
	}
	if cfg.RecyclarrPath == "" {
		cfg.RecyclarrPath = filepath.Join(filepath.Dir(cfg.SettingsPath), "config", "recyclarr", "recyclarr.yml")
	}

	sonarr := newServarr("sonarr", cfg.Endpoints.Sonarr, "v3", cfg.SonarrAPIKey, cfg.HTTPClient)
	radarr := newServarr("radarr", cfg.Endpoints.Radarr, "v3", cfg.RadarrAPIKey, cfg.HTTPClient)
	filmSwitch := settings.UHDFilms != saved.Settings.UHDFilms
	var seerr *apiClient
	var filmService map[string]any
	var previousRules []map[string]any
	if filmSwitch {
		// Check credentials and the target service before writing YAML or
		// changing any app. An ordinary save does not require Seerr.
		seerr, err = mediaSeerrClient(cfg)
		if err != nil {
			return fmt.Errorf("switch film profile in Seerr: %w", err)
		}
		filmService, err = seerrFilmService(ctx, seerr)
		if err != nil {
			return fmt.Errorf("switch film profile in Seerr: %w", err)
		}
		if err = seerr.do(ctx, http.MethodGet, "/overrideRule", nil, &previousRules); err != nil {
			return err
		}
		if settings.UHDFilms {
			saved.PreviousFilmProfile = &seerrProfile{ID: jsonInt(filmService["activeProfileId"])}
			saved.PreviousFilmProfile.Name, _ = filmService["activeProfileName"].(string)
		} else {
			var profiles []seerrProfile
			if err = radarr.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
				return err
			}
			if !slices.ContainsFunc(profiles, func(p seerrProfile) bool {
				return p.ID > 0 && (p.Name == filmProfileName(false) || saved.PreviousFilmProfile != nil && p.ID == saved.PreviousFilmProfile.ID)
			}) {
				return errors.New("Radarr has neither the previous film profile nor HD Bluray + WEB")
			}
		}
	}
	rollbackFilm, err := applyFilmConfig(ctx, settings.UHDFilms, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, rollbackFilm())
		}
	}()
	results := make([]mediaServarrResult, 2)
	errs := make([]error, 2)
	cfg.Logf("Applying Sonarr and Radarr media languages")
	var wg sync.WaitGroup
	for i, app := range []*servarr{sonarr, radarr} {
		wg.Go(func() {
			source := "WEB-1080p"
			if i == 1 {
				source = filmProfileName(settings.UHDFilms)
			}
			target := dutchProfile
			if i == 1 && settings.UHDFilms {
				target = dutch4KProfile
			}
			results[i], errs[i] = applyMediaServarr(ctx, app, source, target)
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	cfg.Logf("Applying Bazarr subtitle languages")
	b := &bazarr{baseURL: strings.TrimRight(cfg.Endpoints.Bazarr, "/"), apiKey: cfg.BazarrAPIKey, http: cfg.HTTPClient}
	if err := applyMediaBazarr(ctx, b, settings); err != nil {
		return err
	}
	cfg.Logf("Applying Seerr Dutch language rules")
	warn := func(err error) {
		cfg.Logf("Could not add the Dutch rule in Seerr: %v. Add it in Seerr under Settings, Override Rules.", err)
	}
	if seerr == nil {
		var seerrErr error
		seerr, seerrErr = mediaSeerrClient(cfg)
		if seerrErr != nil {
			warn(seerrErr)
		}
	}
	if seerr != nil {
		if filmSwitch {
			var rollbackSeerr func() error
			rollbackSeerr, err = switchSeerrFilms(ctx, seerr, radarr, filmService, settings.UHDFilms, saved.PreviousFilmProfile)
			if err != nil {
				return fmt.Errorf("switch film profile in Seerr: %w", err)
			}
			defer func() {
				if err != nil {
					err = errors.Join(err, rollbackSeerr())
				}
			}()
			var previousRule map[string]any
			for _, rule := range previousRules {
				if saved.SeerrRuleIDs["radarr"] > 0 && jsonInt(rule["id"]) == saved.SeerrRuleIDs["radarr"] {
					previousRule = rule
				}
			}
			defer func() {
				if err != nil && saved.SeerrRuleIDs["radarr"] > 0 {
					restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
					defer cancel()
					path := fmt.Sprintf("/overrideRule/%d", saved.SeerrRuleIDs["radarr"])
					if previousRule != nil {
						err = errors.Join(err, seerr.do(restoreCtx, http.MethodPut, path, previousRule, nil))
					} else {
						err = errors.Join(err, seerr.do(restoreCtx, http.MethodDelete, path, nil, nil))
					}
				}
			}()
		}
		if saved.SeerrRuleIDs == nil {
			saved.SeerrRuleIDs = map[string]int{}
		}
		for i, kind := range []string{"sonarr", "radarr"} {
			id, ruleErr := ensureDutchRule(ctx, seerr, kind, results[i], saved.SeerrRuleIDs[kind])
			if ruleErr != nil {
				warn(ruleErr)
			}
			if id > 0 {
				saved.SeerrRuleIDs[kind] = id
			}
		}
	}
	if filmSwitch && !settings.UHDFilms {
		saved.PreviousFilmProfile = nil
	}
	previousSizes := saved.Settings.Sizes
	saved.Version, saved.Settings = 1, settings
	data, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	existing, err := os.ReadFile(cfg.SettingsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	rollback, err := applyMediaSizes(ctx, settings.Sizes, previousSizes, cfg, filmSwitch && !settings.UHDFilms)
	if err != nil {
		return err
	}
	cfg.Logf("Saving the media settings")
	if !bytes.Equal(existing, data) {
		if err := writeFileAtomic(cfg.SettingsPath, data, 0o644); err != nil {
			return errors.Join(fmt.Errorf("save media settings: %w", err), rollback())
		}
	}
	if !settings.UHDFilms && filmSwitch && seerr != nil {
		if cleanupErr := cleanFilmProfiles(ctx, radarr, seerr, cfg.Logf); cleanupErr != nil {
			cfg.Logf("Could not finish checking unused 4K film profiles: %v", cleanupErr)
		}
	}
	return nil
}

// Seerr v3.5.1 commit 564bfb08, checked 7 November 2026: settings/index.ts
// stores main.apiKey in settings.json. middleware/auth.ts accepts X-API-Key.
func mediaSeerrAPIKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var config struct {
		Main struct {
			APIKey string `json:"apiKey"`
		} `json:"main"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", errors.New("invalid Seerr settings JSON")
	}
	if config.Main.APIKey == "" {
		return "", errors.New("Seerr settings have no main.apiKey")
	}
	return config.Main.APIKey, nil
}

type mediaServarrResult struct{ tag, profile int }

// Sonarr v4.0.20.3014 and Radarr v6.4.4.10685, checked 7 November 2026:
// LanguageSpecification has implementation LanguageSpecification, name Language,
// fields value and exceptLanguage, and Dutch has ID 7. negate belongs to the
// specification. CustomFormatController and QualityProfileController expose
// GET/POST and PUT /{id} under /api/v3. QualityProfileResource uses formatItems
// with format, name and score. Keep all other profile fields from the GET.
func applyMediaServarr(ctx context.Context, app *servarr, sourceName string, targets ...string) (mediaServarrResult, error) {
	targetName := dutchProfile
	if len(targets) > 0 {
		targetName = targets[0]
	}
	var result mediaServarrResult
	tag, err := app.ensureTag(ctx, "dutch")
	if err != nil {
		return result, err
	}
	result.tag = tag
	var formats []map[string]any
	if err := app.do(ctx, http.MethodGet, "/customformat", nil, &formats); err != nil {
		return result, err
	}
	scores := map[int]int{}
	for _, wanted := range []struct {
		name   string
		negate bool
		score  int
	}{
		{dutchAudioFormat, false, 100}, {notDutchAudioFormat, true, -10000},
	} {
		body := map[string]any{"name": wanted.name, "includeCustomFormatWhenRenaming": false,
			"specifications": []any{map[string]any{"name": "Dutch", "implementation": "LanguageSpecification", "implementationName": "Language", "negate": wanted.negate, "required": false,
				"fields": []any{map[string]any{"name": "value", "value": 7}, map[string]any{"name": "exceptLanguage", "value": false}},
			}},
		}
		index := slices.IndexFunc(formats, func(f map[string]any) bool { return f["name"] == wanted.name })
		if index < 0 {
			var added map[string]any
			if err := app.do(ctx, http.MethodPost, "/customformat", body, &added); err != nil {
				return result, err
			}
			formats = append(formats, added)
			index = len(formats) - 1
		} else if !matchesDutchFormat(formats[index], wanted.negate) {
			body["id"] = formats[index]["id"]
			if err := app.do(ctx, http.MethodPut, fmt.Sprintf("/customformat/%d", jsonInt(body["id"])), body, nil); err != nil {
				return result, err
			}
		}
		id := jsonInt(formats[index]["id"])
		if id <= 0 {
			return result, fmt.Errorf("%s: custom format %s has no ID", app.name, wanted.name)
		}
		scores[id] = wanted.score
	}
	var profiles []map[string]any
	if err := app.do(ctx, http.MethodGet, "/qualityprofile", nil, &profiles); err != nil {
		return result, err
	}
	if app.name == "radarr" && targetName == dutch4KProfile {
		source := slices.IndexFunc(profiles, func(p map[string]any) bool { return p["name"] == sourceName })
		if source < 0 {
			return result, fmt.Errorf("%s: missing %s profile; run Recyclarr sync first", app.name, sourceName)
		}
		// Keep the codec penalties from Recyclarr, including on a Dutch 4K
		// profile copied before these penalties were corrected. Its other
		// copied scores stay unchanged, as with the existing Dutch profile.
		items, _ := profiles[source]["formatItems"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["name"] == "x265 (HD)" || item["name"] == "x265 (no HDR/DV)" {
				scores[jsonInt(item["format"])] = jsonInt(item["score"])
			}
		}
	}
	index := slices.IndexFunc(profiles, func(p map[string]any) bool { return p["name"] == targetName })
	method, path := http.MethodPut, ""
	var profile map[string]any
	if index >= 0 {
		profile = profiles[index]
		path = fmt.Sprintf("/qualityprofile/%d", jsonInt(profile["id"]))
	} else {
		source := slices.IndexFunc(profiles, func(p map[string]any) bool { return p["name"] == sourceName })
		if source < 0 {
			return result, fmt.Errorf("%s: missing %s profile; run Recyclarr sync first", app.name, sourceName)
		}
		profile = profiles[source]
		delete(profile, "id")
		profile["name"] = targetName
		// Radarr v6.4.4.10685, checked 7 November 2026: its download
		// LanguageSpecification rejects releases before scores if the
		// profile language disagrees. Dutch (ID 7) allows Dutch dubs too.
		if app.name == "radarr" {
			profile["language"] = map[string]any{"id": 7, "name": "Dutch"}
		}
		// Recyclarr v8.8.0, checked 7 November 2026: UpdatedProfileBuilder
		// visits only planned profiles. deploy/stacks/arr/recyclarr.yml
		// plans the normal and anime profiles, never either Dutch profile.
		// ConfigYamlExtensions defaults delete_old_custom_formats to false,
		// so our two formats and this profile survive its daily sync.
		method, path = http.MethodPost, "/qualityprofile"
	}
	for _, format := range formats {
		if format["name"] == notOriginalFormat {
			if id := jsonInt(format["id"]); id > 0 {
				scores[id] = 0
			}
		}
	}
	items, ok := profile["formatItems"].([]any)
	if !ok {
		return result, fmt.Errorf("%s: quality profile has invalid formatItems", app.name)
	}
	changed := false
	for i, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return result, fmt.Errorf("%s: quality profile has an invalid format item", app.name)
		}
		id := jsonInt(entry["format"])
		if score, ok := scores[id]; ok {
			if jsonInt(entry["score"]) != score {
				entry["score"] = score
				items[i] = entry
				changed = true
			}
			delete(scores, id)
		}
	}
	// Both APIs require every custom format in the profile, including formats
	// just created. Scores outside the managed language and codec entries stay.
	for _, format := range formats {
		id := jsonInt(format["id"])
		if score, ok := scores[id]; ok {
			items = append(items, map[string]any{"format": id, "name": format["name"], "score": score})
			changed = true
		}
	}
	profile["formatItems"] = items
	if method == http.MethodPost {
		var added map[string]any
		if err := app.do(ctx, method, path, profile, &added); err != nil {
			return result, err
		}
		profile = added
	} else if changed {
		if err := app.do(ctx, method, path, profile, nil); err != nil {
			return result, err
		}
	}
	result.profile = jsonInt(profile["id"])
	if result.profile <= 0 {
		return result, fmt.Errorf("%s: Dutch quality profile has no ID", app.name)
	}
	return result, nil
}

func matchesDutchFormat(format map[string]any, negate bool) bool {
	if format["includeCustomFormatWhenRenaming"] == true {
		return false
	}
	specs, _ := format["specifications"].([]any)
	if len(specs) != 1 {
		return false
	}
	spec, _ := specs[0].(map[string]any)
	if spec["implementation"] != "LanguageSpecification" || spec["negate"] != negate || spec["required"] == true {
		return false
	}
	fields, _ := spec["fields"].([]any)
	found := false
	for _, raw := range fields {
		field, _ := raw.(map[string]any)
		if field["name"] == "value" {
			found = jsonInt(field["value"]) == 7
		}
		if field["name"] == "exceptLanguage" && field["value"] == true {
			return false
		}
	}
	return found
}

func jsonInt(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func sameJSON(a, b any) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(aa, bb)
}

// Seerr v3.5.1 commit 564bfb08, checked 7 November 2026: OverrideRule.ts and
// routes/overrideRule.ts use language, profileId, tags as comma-separated IDs,
// and sonarrServiceId or radarrServiceId. lib/overrideRules.ts matches language
// against TMDB original_language. Service ID zero is valid. Existing constrained
// rules stay intact. Anime TV keeps Seerr's built-in anime handling.
func ensureDutchRule(ctx context.Context, seerr *apiClient, kind string, result mediaServarrResult, ownedID int) (int, error) {
	var services []seerrService
	if err := seerr.do(ctx, http.MethodGet, "/settings/"+kind, nil, &services); err != nil {
		return ownedID, err
	}
	index := slices.IndexFunc(services, func(s seerrService) bool { return s.Hostname == kind })
	if index < 0 {
		return ownedID, fmt.Errorf("Seerr has no %s service", kind)
	}
	field := kind + "ServiceId"
	rule := map[string]any{field: services[index].ID, "language": "nl", "profileId": result.profile, "tags": strconv.Itoa(result.tag)}
	var rules []map[string]any
	if err := seerr.do(ctx, http.MethodGet, "/overrideRule", nil, &rules); err != nil {
		return ownedID, err
	}
	var owned map[string]any
	var conflict error
	for _, existing := range rules {
		if ownedID > 0 && jsonInt(existing["id"]) == ownedID {
			owned = existing
			continue
		}
		if existing[field] != nil && existing["language"] == "nl" {
			conflict = fmt.Errorf("another Dutch %s rule exists; leaving it unchanged", kind)
		}
	}
	if owned != nil {
		desired := maps.Clone(owned)
		for key, value := range rule {
			desired[key] = value
		}
		tags, _ := owned["tags"].(string)
		if !slices.Contains(strings.Split(tags, ","), strconv.Itoa(result.tag)) {
			tags = strings.Trim(tags+","+strconv.Itoa(result.tag), ",")
		}
		desired["tags"] = tags
		if !sameJSON(owned, desired) {
			if err := seerr.do(ctx, http.MethodPut, fmt.Sprintf("/overrideRule/%d", ownedID), desired, nil); err != nil {
				return ownedID, err
			}
		}
		return ownedID, conflict
	}
	if conflict != nil {
		return ownedID, conflict
	}
	var added struct {
		ID int `json:"id"`
	}
	if err := seerr.do(ctx, http.MethodPost, "/overrideRule", rule, &added); err != nil {
		return ownedID, err
	}
	if added.ID <= 0 {
		return ownedID, errors.New("Seerr returned a Dutch rule without an ID")
	}
	return added.ID, nil
}

// Bazarr v1.6.2, checked 7 November 2026: system/settings.py replaces the whole
// languages-profiles list, so preserve profile 1 and every unrelated profile.
// system/languages_profiles.py returns the list directly. Profile tag is a tag
// label, used by both sync/parser.py files when serie_tag_enabled and
// movie_tag_enabled are true. app/config.py clears arrays with one empty value.
func applyMediaBazarr(ctx context.Context, b *bazarr, settings MediaSettings) error {
	api := &apiClient{name: "bazarr", baseURL: b.baseURL + "/api", headers: map[string]string{"X-API-KEY": b.apiKey}, http: b.http}
	var profiles []map[string]any
	if err := api.do(ctx, http.MethodGet, "/system/languages/profiles", nil, &profiles); err != nil {
		return err
	}
	var current map[string]map[string]any
	if err := api.do(ctx, http.MethodGet, "/system/settings", nil, &current); err != nil {
		return err
	}
	var languages []struct {
		Code    string `json:"code2"`
		Enabled bool   `json:"enabled"`
	}
	if err := api.do(ctx, http.MethodGet, "/system/languages", nil, &languages); err != nil {
		return err
	}
	enabled := []string{}
	for _, language := range languages {
		if language.Enabled {
			enabled = append(enabled, language.Code)
		}
	}
	profilesChanged := false
	ids := map[string]int{}
	nextID := 2
	for _, p := range profiles {
		if id := jsonInt(p["profileId"]); id >= nextID {
			nextID = id + 1
		}
	}
	for _, kind := range settings.kinds() {
		name := kind.name + " (Iglo-OS)"
		index := slices.IndexFunc(profiles, func(p map[string]any) bool { return p["name"] == name })
		if kind.name == "Series" {
			index = slices.IndexFunc(profiles, func(p map[string]any) bool { return jsonInt(p["profileId"]) == 1 })
			if index < 0 {
				return errors.New("Bazarr has no language profile 1")
			}
		} else if kind.name == "Films" && sameMediaLanguages(settings.Films.Subtitles, settings.Series.Subtitles) {
			ids[kind.name] = 1
			continue
		} else if (kind.name == "Anime" || kind.name == "Dutch") && len(kind.settings.Subtitles) == 0 {
			if index >= 0 && profiles[index]["tag"] != nil {
				profiles[index]["tag"] = nil
				profilesChanged = true
			}
			continue
		}
		var profile map[string]any
		if index >= 0 {
			profile = profiles[index]
		} else {
			raw, err := bazarrLanguageProfile(kind.settings.Subtitles)
			if err != nil {
				return err
			}
			var built []map[string]any
			if err := json.Unmarshal([]byte(raw), &built); err != nil {
				return err
			}
			profile = built[0]
			profile["profileId"], profile["name"] = nextID, name
			nextID++
		}
		desired, err := mediaBazarrProfile(profile, kind.settings.Subtitles)
		if err != nil {
			return err
		}
		if kind.name == "Anime" || kind.name == "Dutch" {
			desired["tag"] = strings.ToLower(kind.name)
		}
		ids[kind.name] = jsonInt(desired["profileId"])
		if index < 0 {
			profiles = append(profiles, desired)
			profilesChanged = true
		} else if !sameJSON(profile, desired) {
			profiles[index] = desired
			profilesChanged = true
		}
		for _, language := range kind.settings.Subtitles {
			if !slices.Contains(enabled, language) {
				enabled = append(enabled, language)
			}
		}
	}

	form := url.Values{}
	if profilesChanged {
		data, _ := json.Marshal(profiles)
		form.Set("languages-profiles", string(data))
	}
	for _, language := range languages {
		if slices.Contains(enabled, language.Code) && !language.Enabled {
			form["languages-enabled"] = enabled
			break
		}
	}
	setValue := func(section, key string, want any) {
		if !sameJSON(current[section][key], want) {
			form.Set("settings-"+section+"-"+key, fmt.Sprint(want))
		}
	}
	for _, kind := range []struct{ name, key string }{{"Series", "serie"}, {"Films", "movie"}} {
		id := ids[kind.name]
		setValue("general", kind.key+"_default_enabled", true)
		// Keep a default chosen by the user. Profile 1 is the stack default.
		previous := current["general"][kind.key+"_default_profile"]
		ownedDefault := fmt.Sprint(previous) == "1"
		if kind.name == "Films" {
			for _, profile := range profiles {
				if profile["name"] == "Films (Iglo-OS)" && fmt.Sprint(previous) == fmt.Sprint(profile["profileId"]) {
					ownedDefault = true
				}
			}
		}
		if id > 0 && (previous == nil || previous == "" || ownedDefault) {
			if fmt.Sprint(previous) != strconv.Itoa(id) {
				form.Set("settings-general-"+kind.key+"_default_profile", strconv.Itoa(id))
			}
		}
		if len(settings.Anime.Subtitles) > 0 || len(settings.Dutch.Subtitles) > 0 {
			setValue("general", kind.key+"_tag_enabled", true)
		}
	}
	setList := func(section, key, entry string, include bool) {
		// Read a pending edit too, since two owned tags may share one field.
		field := "settings-" + section + "-" + key
		values, pending := form[field]
		if !pending {
			values = []string{}
			if raw, ok := current[section][key].([]any); ok {
				for _, v := range raw {
					values = append(values, fmt.Sprint(v))
				}
			}
		}
		values = slices.DeleteFunc(values, func(v string) bool { return v == "" })
		original := slices.Clone(values)
		if include {
			if !slices.Contains(values, entry) {
				values = append(values, entry)
			}
		} else {
			values = slices.DeleteFunc(values, func(v string) bool { return v == entry })
		}
		if !slices.Equal(values, original) || pending {
			if len(values) == 0 {
				values = []string{""}
			}
			form[field] = values
		}
	}
	if len(settings.Anime.Subtitles) > 0 {
		setList("general", "remove_profile_tags", "anime", false)
	}
	if len(settings.Dutch.Subtitles) > 0 {
		setList("general", "remove_profile_tags", "dutch", false)
	}
	setList("sonarr", "excluded_series_types", "anime", len(settings.Anime.Subtitles) == 0)
	setList("sonarr", "excluded_tags", "anime", len(settings.Anime.Subtitles) == 0)
	setList("radarr", "excluded_tags", "anime", len(settings.Anime.Subtitles) == 0)
	for _, section := range []string{"sonarr", "radarr"} {
		setList(section, "excluded_tags", "dutch", len(settings.Dutch.Subtitles) == 0)
	}
	if len(form) > 0 {
		writer := *b
		if form.Has("languages-profiles") {
			writer.http = mediaBazarrWriteClient(b.http)
		}
		if err := writer.saveSettings(ctx, form); err != nil {
			return err
		}
	}
	return assignMediaBazarr(ctx, b, api, ids, profiles)
}

// mediaBazarrProfile keeps all selected items in their stored order and appends
// new codes with flags off. Bazarr v1.6.2, checked 7 November 2026:
// app/database.py uses item IDs for cutoff and 65535 for all items.
// api/system/settings.py stores JSON null as no cutoff.
func mediaBazarrProfile(profile map[string]any, languages []string) (map[string]any, error) {
	desired := maps.Clone(profile)
	old, ok := profile["items"].([]any)
	if !ok && profile["items"] != nil {
		return nil, errors.New("Bazarr profile has invalid items")
	}
	items := []any{}
	seen := map[string]bool{}
	nextID := 1
	cutoff := jsonInt(profile["cutoff"])
	keptCutoff := cutoff == 65535
	for _, raw := range old {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("Bazarr profile has an invalid language item")
		}
		id := jsonInt(item["id"])
		if id >= nextID {
			nextID = id + 1
		}
		code, _ := item["language"].(string)
		if slices.Contains(languages, code) {
			items = append(items, maps.Clone(item))
			seen[code] = true
			keptCutoff = keptCutoff || id == cutoff
		}
	}
	for _, code := range languages {
		if !seen[code] {
			items = append(items, map[string]any{
				"id": nextID, "language": code, "hi": "False", "forced": "False",
				"audio_exclude": "False", "audio_only_include": "False",
			})
			nextID++
		}
	}
	if !keptCutoff {
		desired["cutoff"] = nil
	}
	desired["items"] = items
	return desired, nil
}

// Bazarr v1.6.2, checked 7 November 2026: settings and title assignment
// handlers rescan subtitles before replying. Keep other calls at their normal
// timeout. Copy the client so concurrent calls do not change each other's limit.
func mediaBazarrWriteClient(client *http.Client) *http.Client {
	writer := *client
	writer.Timeout = 10 * time.Minute
	return &writer
}

func sameMediaLanguages(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, code := range a {
		if !slices.Contains(b, code) {
			return false
		}
	}
	return true
}

// Bazarr v1.6.2, checked 7 November 2026: series.py and movies.py return data
// lists and accept aligned form lists seriesid/radarrid and profileid on POST.
// Only profile 1 and named Iglo-OS profiles can be reassigned, even with tags.
func assignMediaBazarr(ctx context.Context, b *bazarr, api *apiClient, ids map[string]int, profiles []map[string]any) error {
	owned := map[int]bool{1: true}
	for _, p := range profiles {
		for _, name := range []string{"Anime", "Dutch", "Series", "Films"} {
			if p["name"] == name+" (Iglo-OS)" {
				owned[jsonInt(p["profileId"])] = true
			}
		}
	}
	for _, kind := range []struct{ path, normal, field string }{{"series", "Series", "seriesid"}, {"movies", "Films", "radarrid"}} {
		var titles struct {
			Data []struct {
				ID         int      `json:"sonarrSeriesId"`
				MovieID    int      `json:"radarrId"`
				Profile    *int     `json:"profileId"`
				Tags       []string `json:"tags"`
				SeriesType string   `json:"seriesType"`
			} `json:"data"`
		}
		if err := api.do(ctx, http.MethodGet, "/"+kind.path, nil, &titles); err != nil {
			return err
		}
		form := url.Values{}
		flush := func() error {
			if len(form) == 0 {
				return nil
			}
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/"+kind.path, strings.NewReader(form.Encode()))
			if err != nil {
				return err
			}
			request.Header.Set("X-API-KEY", b.apiKey)
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response, err := mediaBazarrWriteClient(b.http).Do(request)
			if err != nil {
				return fmt.Errorf("bazarr %s profiles: %w", kind.path, err)
			}
			response.Body.Close()
			if response.StatusCode >= 300 {
				return fmt.Errorf("bazarr %s profiles: %s", kind.path, response.Status)
			}
			form = url.Values{}
			return nil
		}
		for _, title := range titles.Data {
			if title.Profile == nil || !owned[*title.Profile] {
				continue
			}
			name := kind.normal
			if slices.Contains(title.Tags, "anime") || title.SeriesType == "anime" {
				name = "Anime"
			} else if slices.Contains(title.Tags, "dutch") {
				name = "Dutch"
			}
			desired := ids[name]
			if desired <= 0 || *title.Profile == desired {
				continue
			}
			id := title.ID
			if kind.path == "movies" {
				id = title.MovieID
			}
			form.Add(kind.field, strconv.Itoa(id))
			form.Add("profileid", strconv.Itoa(desired))
			if len(form["profileid"]) == 50 {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		if err := flush(); err != nil {
			return err
		}
	}
	return nil
}

// MediaSettingsPaths locates state and app credentials next to the chosen env.
func MediaSettingsPaths(envPath string) (settings, bazarrConfig, seerrConfig string) {
	dir := filepath.Dir(envPath)
	return filepath.Join(dir, "iglo-media.json"), filepath.Join(dir, "config/bazarr/config/config.yaml"), filepath.Join(dir, "config/seerr/settings.json")
}
