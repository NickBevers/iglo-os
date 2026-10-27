package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSeerr struct {
	mu          sync.Mutex
	initialized bool
	calls       []string
	bodies      map[string]map[string]any
	radarr      []map[string]any
	rules       []map[string]any
	// noAnimeFilms leaves the anime profile out of Radarr, like before Recyclarr ran.
	noAnimeFilms bool
	// noTVDB fails the metadata change, like Seerr does when TheTVDB can't be reached.
	noTVDB bool
}

func (f *fakeSeerr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	call := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v1")
	f.calls = append(f.calls, call)
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	f.bodies[call] = body

	// Everything after the login needs the session cookie.
	if call != "GET /settings/public" && call != "POST /auth/jellyfin" {
		if cookie, err := r.Cookie("connect.sid"); err != nil || cookie.Value != "admin" {
			http.Error(w, "not logged in", http.StatusForbidden)
			return
		}
	}

	switch call {
	case "GET /settings/public":
		json.NewEncoder(w).Encode(map[string]bool{"initialized": f.initialized})
	case "POST /auth/jellyfin":
		http.SetCookie(w, &http.Cookie{Name: "connect.sid", Value: "admin", Path: "/"})
		w.Write([]byte(`{"id":1}`))
	case "POST /settings/jellyfin/library/sync":
		w.Write([]byte(`[{"id":"m1","type":"movie"},{"id":"s1","type":"show"},{"id":"x1","type":"music"}]`))
	case "GET /settings/radarr":
		json.NewEncoder(w).Encode(f.radarr)
	case "GET /settings/sonarr":
		w.Write([]byte(`[]`))
	case "POST /settings/radarr/test":
		w.Write([]byte(`{"profiles":` + f.radarrProfiles() + `}`))
	case "GET /settings/radarr/0/profiles":
		w.Write([]byte(f.radarrProfiles()))
	case "POST /settings/radarr":
		// Seerr numbers its first Radarr 0.
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":0,"hostname":"radarr"}`))
	case "GET /overrideRule":
		json.NewEncoder(w).Encode(append([]map[string]any{}, f.rules...))
	case "PUT /settings/metadatas":
		if f.noTVDB {
			http.Error(w, `{"message":"Failed to test metadata provider"}`, http.StatusInternalServerError)
		}
	case "POST /overrideRule":
		f.rules = append(f.rules, body)
		json.NewEncoder(w).Encode(body)
	case "POST /settings/sonarr/test":
		w.Write([]byte(`{"profiles":[{"id":1,"name":"Any"},{"id":4,"name":"HD-1080p"},{"id":9,"name":"[Anime] Remux-1080p"}]}`))
	default:
		w.Write([]byte(`{}`))
	}
}

func (f *fakeSeerr) radarrProfiles() string {
	if f.noAnimeFilms {
		return `[{"id":1,"name":"Any"},{"id":7,"name":"HD Bluray + WEB"}]`
	}

	return `[{"id":1,"name":"Any"},{"id":7,"name":"HD Bluray + WEB"},{"id":12,"name":"[Anime] Remux-1080p"}]`
}

func seerrConfig() Config {
	return Config{
		RadarrAPIKey: "radarr-key", SonarrAPIKey: "sonarr-key",
		JellyfinURL:           "http://192.168.1.20:8096",
		JellyfinAdminUsername: "dries", JellyfinAdminPassword: "secret",
		SeriesFolder: "TV Shows",
		AnimeFolder:  "Anime", AnimeMoviesFolder: "Anime Movies",
		Logf: func(string, ...any) {},
	}
}

func TestConfigureSeerr(t *testing.T) {
	fake := &fakeSeerr{bodies: map[string]map[string]any{}}
	server := httptest.NewServer(fake)
	defer server.Close()

	if err := configureSeerr(context.Background(), seerrConfig(), newSeerr(server.URL, 5*time.Second), animeTags{Sonarr: 2, Radarr: 5}); err != nil {
		t.Fatal(err)
	}

	login := fake.bodies["POST /auth/jellyfin"]
	if login["username"] != "dries" || login["hostname"] != "192.168.1.20" || login["port"] != float64(8096) || login["serverType"] != float64(mediaServerJellyfin) {
		t.Fatalf("login = %v", login)
	}
	for _, call := range []string{"PUT /settings/jellyfin/library/m1", "PUT /settings/jellyfin/library/s1", "POST /settings/radarr", "POST /settings/sonarr", "POST /settings/initialize"} {
		if !slices.Contains(fake.calls, call) {
			t.Errorf("missing %s in %v", call, fake.calls)
		}
	}
	if slices.Contains(fake.calls, "PUT /settings/jellyfin/library/x1") {
		t.Error("turned on a music library")
	}

	radarr := fake.bodies["POST /settings/radarr"]
	if radarr["hostname"] != "radarr" || radarr["activeProfileId"] != float64(7) || radarr["activeDirectory"] != "/data/media/movies" || radarr["apiKey"] != "radarr-key" {
		t.Fatalf("radarr = %v", radarr)
	}
	// Anime films go to their own profile with the anime tag, by a rule on Radarr 0.
	if len(fake.rules) != 1 || fake.rules[0]["radarrServiceId"] != float64(0) || fake.rules[0]["keywords"] != "210024" ||
		fake.rules[0]["profileId"] != float64(12) || fake.rules[0]["tags"] != "5" || fake.rules[0]["rootFolder"] != "/data/media/Anime Movies" {
		t.Fatalf("rules = %v", fake.rules)
	}
	if metadata := fake.bodies["PUT /settings/metadatas"]; metadata["anime"] != "tvdb" || metadata["tv"] != "tmdb" {
		t.Fatalf("metadata = %v", metadata)
	}
	// No WEB-1080p yet, so the default of Sonarr.
	sonarr := fake.bodies["POST /settings/sonarr"]
	if sonarr["activeProfileName"] != "HD-1080p" || sonarr["activeDirectory"] != "/data/media/TV Shows" || sonarr["enableSeasonFolders"] != true ||
		sonarr["activeAnimeProfileId"] != float64(9) || sonarr["activeAnimeDirectory"] != "/data/media/Anime" ||
		!slices.Equal(sonarr["animeTags"].([]any), []any{float64(2)}) {
		t.Fatalf("sonarr = %v", sonarr)
	}
}

func TestConfigureSeerrSkipsWhatIsDone(t *testing.T) {
	fake := &fakeSeerr{bodies: map[string]map[string]any{}, initialized: true}
	server := httptest.NewServer(fake)
	defer server.Close()

	if err := configureSeerr(context.Background(), seerrConfig(), newSeerr(server.URL, 5*time.Second), animeTags{}); err != nil {
		t.Fatal(err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("an initialized Seerr got calls: %v", fake.calls)
	}

	// Not initialized, but Radarr is already there: only Sonarr is added.
	fake = &fakeSeerr{bodies: map[string]map[string]any{}, radarr: []map[string]any{{"hostname": "radarr"}}}
	server2 := httptest.NewServer(fake)
	defer server2.Close()
	if err := configureSeerr(context.Background(), seerrConfig(), newSeerr(server2.URL, 5*time.Second), animeTags{}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(fake.calls, "POST /settings/radarr") || !slices.Contains(fake.calls, "POST /settings/sonarr") {
		t.Fatalf("calls = %v", fake.calls)
	}
}

func TestAnimeFilmRule(t *testing.T) {
	// Radarr is already there, with its rule: nothing is added twice.
	zero := float64(0)
	fake := &fakeSeerr{bodies: map[string]map[string]any{}, radarr: []map[string]any{{"id": 0, "hostname": "radarr"}},
		rules: []map[string]any{{"radarrServiceId": zero, "keywords": "1,210024"}}}
	server := httptest.NewServer(fake)
	defer server.Close()
	if err := configureSeerr(context.Background(), seerrConfig(), newSeerr(server.URL, 5*time.Second), animeTags{}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(fake.calls, "POST /overrideRule") {
		t.Fatalf("added a second rule: %v", fake.rules)
	}

	// A rule for another Radarr doesn't count.
	fake = &fakeSeerr{bodies: map[string]map[string]any{}, rules: []map[string]any{{"radarrServiceId": 3, "keywords": "210024"}}}
	server2 := httptest.NewServer(fake)
	defer server2.Close()
	if err := configureSeerr(context.Background(), seerrConfig(), newSeerr(server2.URL, 5*time.Second), animeTags{}); err != nil {
		t.Fatal(err)
	}
	if len(fake.rules) != 2 || fake.rules[1]["tags"] != nil {
		t.Fatalf("rules = %v", fake.rules)
	}
	if tags := fake.bodies["POST /settings/sonarr"]["animeTags"].([]any); len(tags) != 0 {
		t.Fatalf("anime tags without a tag = %v", tags)
	}

	// Before Recyclarr made the profile, anime films keep the normal one and
	// the setup goes on.
	var logs []string
	cfg := seerrConfig()
	cfg.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	fake = &fakeSeerr{bodies: map[string]map[string]any{}, noAnimeFilms: true, noTVDB: true}
	server3 := httptest.NewServer(fake)
	defer server3.Close()
	if err := configureSeerr(context.Background(), cfg, newSeerr(server3.URL, 5*time.Second), animeTags{}); err != nil {
		t.Fatal(err)
	}
	if len(fake.rules) != 0 || !slices.Contains(fake.calls, "POST /settings/initialize") {
		t.Fatalf("rules = %v, calls = %v", fake.rules, fake.calls)
	}
	for _, want := range []string{"no [Anime] Remux-1080p profile yet", "Could not set TheTVDB for anime"} {
		if !slices.ContainsFunc(logs, func(line string) bool { return strings.Contains(line, want) }) {
			t.Fatalf("no %q in logs %v", want, logs)
		}
	}
}

func TestSplitURL(t *testing.T) {
	for raw, want := range map[string]struct {
		host string
		port int
		ssl  bool
	}{
		"http://192.168.1.20:8096":   {"192.168.1.20", 8096, false},
		"https://jellyfin.lan":       {"jellyfin.lan", 443, true},
		"http://jellyfin.lan/":       {"jellyfin.lan", 8096, false},
		"https://10.0.0.5:8920/path": {"10.0.0.5", 8920, true},
	} {
		host, port, ssl, err := splitURL(raw)
		if err != nil || host != want.host || port != want.port || ssl != want.ssl {
			t.Errorf("%s: %s %d %v %v", raw, host, port, ssl, err)
		}
	}
	if _, _, _, err := splitURL("not a url"); err == nil {
		t.Error("no error for a bad URL")
	}
}
