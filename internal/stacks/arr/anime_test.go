package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// fakeTagged serves the tags and delay profiles of Sonarr or Radarr.
type fakeTagged struct {
	tags     []map[string]any
	profiles []map[string]any
	broken   bool
}

func (f *fakeTagged) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.broken {
		http.Error(w, "down", http.StatusInternalServerError)
		return
	}

	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	switch r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v3") {
	case "GET /tag":
		json.NewEncoder(w).Encode(f.tags)
	case "POST /tag":
		body["id"] = len(f.tags) + 3
		f.tags = append(f.tags, body)
		json.NewEncoder(w).Encode(body)
	case "GET /delayprofile":
		json.NewEncoder(w).Encode(f.profiles)
	case "POST /delayprofile":
		f.profiles = append(f.profiles, body)
		json.NewEncoder(w).Encode(body)
	default:
		http.NotFound(w, r)
	}
}

func TestConfigureAnime(t *testing.T) {
	sonarrFake := &fakeTagged{profiles: []map[string]any{{"id": 1, "tags": []int{}}}}
	radarrFake := &fakeTagged{tags: []map[string]any{{"id": 7, "label": "anime"}}}
	sonarrServer, radarrServer := httptest.NewServer(sonarrFake), httptest.NewServer(radarrFake)
	defer sonarrServer.Close()
	defer radarrServer.Close()
	sonarr := newServarr("sonarr", sonarrServer.URL, "v3", "key", sonarrServer.Client())
	radarr := newServarr("radarr", radarrServer.URL, "v3", "key", radarrServer.Client())

	// Twice: the second run adds nothing.
	for range 2 {
		tags := configureAnime(context.Background(), seerrConfig(), sonarr, radarr)
		if tags.Sonarr != 3 || tags.Radarr != 7 {
			t.Fatalf("tags = %+v", tags)
		}
	}
	if len(sonarrFake.tags) != 1 || len(radarrFake.tags) != 1 || len(sonarrFake.profiles) != 2 {
		t.Fatalf("sonarr tags %v, radarr tags %v, delay profiles %v", sonarrFake.tags, radarrFake.tags, sonarrFake.profiles)
	}
	delay := sonarrFake.profiles[1]
	if delay["torrentDelay"] != float64(30) || delay["minimumCustomFormatScore"] != float64(400) ||
		delay["bypassIfAboveCustomFormatScore"] != true || !slices.Equal(delay["tags"].([]any), []any{float64(3)}) {
		t.Fatalf("delay profile = %v", delay)
	}
}

func TestConfigureAnimeOnlyWarns(t *testing.T) {
	broken := httptest.NewServer(&fakeTagged{broken: true})
	defer broken.Close()
	app := newServarr("sonarr", broken.URL, "v3", "key", broken.Client())

	var logs []string
	cfg := seerrConfig()
	cfg.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	if tags := configureAnime(context.Background(), cfg, app, app); tags != (animeTags{}) {
		t.Fatalf("tags = %+v", tags)
	}
	if len(logs) != 2 {
		t.Fatalf("logs = %v", logs)
	}
}
