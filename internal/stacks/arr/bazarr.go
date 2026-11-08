package arr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type bazarr struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// bazarrAPIKey reads auth.apikey from Bazarr's config.yaml, which Bazarr
// writes on first start.
func bazarrAPIKey(configPath string) (string, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	inAuth := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, " ") {
			inAuth = line == "auth:"
			continue
		}
		if key, found := strings.CutPrefix(line, "  apikey: "); inAuth && found {
			return strings.Trim(key, `'"`), nil
		}
	}

	return "", errors.New("bazarr: no auth.apikey in " + configPath)
}

func (b *bazarr) ready(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/api/system/status", nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-API-KEY", b.apiKey)

	response, err := b.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("bazarr: %s", response.Status)
	}

	return nil
}

// saveSettings posts to the same endpoint Bazarr's settings page uses.
func (b *bazarr) saveSettings(ctx context.Context, form url.Values) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/system/settings", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("X-API-KEY", b.apiKey)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := b.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("bazarr settings: %s %s", response.Status, message)
	}

	return nil
}

type bazarrProfileItem struct {
	ID               int    `json:"id"`
	Language         string `json:"language"`
	AudioExclude     string `json:"audio_exclude"`
	AudioOnlyInclude string `json:"audio_only_include"`
	HI               string `json:"hi"`
	Forced           string `json:"forced"`
}

type bazarrProfile struct {
	ProfileID      int                 `json:"profileId"`
	Name           string              `json:"name"`
	Items          []bazarrProfileItem `json:"items"`
	Cutoff         *int                `json:"cutoff"`
	MustContain    []string            `json:"mustContain"`
	MustNotContain []string            `json:"mustNotContain"`
	OriginalFormat bool                `json:"originalFormat"`
	Tag            *string             `json:"tag"`
}

func bazarrLanguageProfile(languages []string) (string, error) {
	for _, language := range languages {
		if !validBazarrLanguage(language) {
			return "", fmt.Errorf("unsupported Bazarr subtitle language %q", language)
		}
	}
	profile := bazarrProfile{ProfileID: 1, Name: "Default", MustContain: []string{}, MustNotContain: []string{}}
	for index, language := range languages {
		profile.Items = append(profile.Items, bazarrProfileItem{
			ID: index + 1, Language: language,
			AudioExclude: "False", AudioOnlyInclude: "False", HI: "False", Forced: "False",
		})
	}

	data, err := json.Marshal([]bazarrProfile{profile})

	return string(data), err
}

// Bazarr v1.6.2, checked 7 November 2026: languages/get_languages.py loads
// ISO 639-1 codes from its bundled pycountry database. custom_lang.py adds
// pb, zt and ea for Brazilian Portuguese, Traditional Chinese and Latin
// American Spanish. Share this set with the media settings validator.
func validBazarrLanguage(code string) bool {
	const codes = " aa ab ae af ak am an ar as av ay az ba be bg bi bm bn bo br bs ca ce ch co cr cs cu cv cy da de dv dz ee el en eo es et eu fa ff fi fj fo fr fy ga gd gl gn gu gv ha he hi ho hr ht hu hy hz ia id ie ig ii ik io is it iu ja jv ka kg ki kj kk kl km kn ko kr ks ku kv kw ky la lb lg li ln lo lt lu lv mg mh mi mk ml mn mr ms mt my na nb nd ne ng nl nn no nr nv ny oc oj om or os pa pi pl ps pt qu rm rn ro ru rw sa sc sd se sg sh si sk sl sm sn so sq sr ss st su sv sw ta te tg th ti tk tl tn to tr ts tt tw ty ug uk ur uz ve vi vo wa wo xh yi yo za zh zu pb zt ea "
	return len(code) == 2 && strings.Contains(codes, " "+code+" ")
}
