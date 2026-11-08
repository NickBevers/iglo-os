package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func libraryAPI(t *testing.T) (RescanConfig, *[]map[string]any) {
	t.Helper()
	commands := []map[string]any{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "test-key" {
			t.Error("missing key")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v3/series":
			json.NewEncoder(w).Encode([]any{map[string]any{"id": 1, "path": "/data/media/anime/Show", "title": "Show", "qualityProfileId": 10}, map[string]any{"id": 2, "path": "/data/media/series/Series", "title": "Series", "qualityProfileId": 20}})
		case "GET /api/v3/movie":
			json.NewEncoder(w).Encode([]any{map[string]any{"id": 3, "path": "/data/media/movies/Film", "title": "Film", "qualityProfileId": 30}, map[string]any{"id": 4, "path": "/data/media/anime-movies/Film", "title": "Anime film", "qualityProfileId": 40}})
		case "GET /api/v3/qualityprofile":
			json.NewEncoder(w).Encode([]any{map[string]any{"id": 10, "formatItems": []any{map[string]any{"format": 1, "score": 0}, map[string]any{"format": 2, "score": 100}}}, map[string]any{"id": 20, "formatItems": []any{map[string]any{"format": 1, "score": 100}}}, map[string]any{"id": 30, "formatItems": []any{map[string]any{"format": 1, "score": -100}}}, map[string]any{"id": 40, "formatItems": []any{map[string]any{"format": 2, "score": 100}}}})
		case "GET /api/v3/customformat":
			json.NewEncoder(w).Encode([]any{map[string]any{"id": 1, "name": "Size", "specifications": []any{map[string]any{"implementation": "SizeSpecification", "negate": true}}}, map[string]any{"id": 2, "name": "Language", "specifications": []any{map[string]any{"implementation": "LanguageSpecification"}}}})
		case "POST /api/v3/command":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			commands = append(commands, body)
			w.WriteHeader(201)
			w.Write([]byte(`{}`))
		default:
			t.Error("unexpected request", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 404)
		}
	})
	return RescanConfig{Endpoints: Endpoints{Sonarr: "http://sonarr", Radarr: "http://radarr"}, SonarrAPIKey: "test-key", RadarrAPIKey: "test-key", HTTPClient: &http.Client{Transport: libraryTransport{handler}}}, &commands
}
func TestRescanMappingAndCommands(t *testing.T) {
	cfg, commands := libraryAPI(t)
	paths := []string{"anime/Show/Season 01/a.mkv", "anime/Show/Season 01/b.mkv", "movies/Film/a.mkv", "anime-movies/Film/a.mkv", "series/Missing/a.mkv", "anime/Show extra/a.mkv"}
	out, err := Rescan(context.Background(), paths, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatal(out)
	}
	want := []map[string]any{{"name": "RescanSeries", "seriesId": float64(1)}, {"name": "RescanMovie", "movieId": float64(3)}, {"name": "RescanMovie", "movieId": float64(4)}}
	if !reflect.DeepEqual(*commands, want) {
		t.Fatalf("commands: %#v", *commands)
	}
	for _, p := range paths {
		mapped, err := MediaPath(p)
		if err != nil || mapped != "/data/media/"+p {
			t.Fatal(mapped, err)
		}
	}
	for _, p := range []string{"../movies/a.mkv", "/movies/a.mkv", "movies/../../etc/passwd", "home-videos/a.mkv", "movies//a.mkv"} {
		if _, err := Rescan(context.Background(), []string{p}, cfg); err == nil {
			t.Fatal("accepted", p)
		}
	}
}
func TestShrinkCheckScoredSizeAndUnmanaged(t *testing.T) {
	cfg, _ := libraryAPI(t)
	out, err := ShrinkCheck(context.Background(), []string{"anime/Show/Season 01/a.mkv", "anime/Show/Season 02/b.mkv", "series/Series/a.mkv", "movies/Film/a.mkv", "anime-movies/Film/a.mkv", "anime/Show extra/a.mkv"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	states := []string{"ok", "refused", "refused", "ok", "ok-unmanaged"}
	if len(out) != len(states) {
		t.Fatal(out)
	}
	for i, s := range states {
		if out[i].State != s {
			t.Fatal(out)
		}
	}
	if len(out[0].Paths) != 2 {
		t.Fatal("not grouped by title")
	}
	if !strings.Contains(out[1].Reason, "SizeSpecification") || out[4].Reason != "not managed by Sonarr or Radarr" {
		t.Fatal(out)
	}
}
func TestShrinkCheckFailsClosed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/series":
			w.Write([]byte(`[{"id":1,"path":"/data/media/anime/Show","qualityProfileId":1}]`))
		case "/api/v3/qualityprofile":
			w.Write([]byte(`[{"id":1,"formatItems":[{"format":4,"score":1}]}]`))
		case "/api/v3/customformat":
			w.Write([]byte(`[]`))
		default:
			http.Error(w, "bad", 500)
		}
	})
	cfg := RescanConfig{Endpoints: Endpoints{Sonarr: "http://sonarr"}, HTTPClient: &http.Client{Transport: libraryTransport{handler}}}
	out, err := ShrinkCheck(context.Background(), []string{"anime/Show/a.mkv"}, cfg)
	if err != nil || out[0].State != "refused" {
		t.Fatal(out, err)
	}
}

// This transport exercises the same HTTP handler without needing a listener.
type libraryTransport struct{ handler http.Handler }

func (t libraryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.handler.ServeHTTP(w, r)
	return w.Result(), nil
}

// Run this integration test outside sandboxes that prohibit loopback binds.
func TestLibraryLoopbackServer(t *testing.T) {
	cfg, _ := libraryAPI(t)
	transport := cfg.HTTPClient.Transport.(libraryTransport)
	server := httptest.NewServer(transport.handler)
	defer server.Close()
	cfg.HTTPClient = nil
	cfg.Endpoints.Sonarr = server.URL
	cfg.Endpoints.Radarr = server.URL
	if _, err := Rescan(context.Background(), []string{"anime/Show/a.mkv", "movies/Film/a.mkv"}, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := ShrinkCheck(context.Background(), []string{"anime/Show/a.mkv", "movies/Film/a.mkv"}, cfg); err != nil {
		t.Fatal(err)
	}
}
