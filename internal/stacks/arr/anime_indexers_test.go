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
)

// fakeIndexerApp serves the indexers, tags, auto tagging rules and series or
// films of Sonarr or Radarr, and applies bulk and editor tag changes.
type fakeIndexerApp struct {
	mu       sync.Mutex
	kind     string
	tags     []map[string]any
	indexers []map[string]any
	items    []map[string]any
	rules    []map[string]any
	writes   []string
}

func prowlarrIndexer(id, prowlarrID int, tags ...int) map[string]any {
	return map[string]any{"id": id, "tags": tags, "fields": []map[string]any{
		{"name": "baseUrl", "value": fmt.Sprintf("http://gluetun:9696/%d/", prowlarrID)},
		{"name": "apiKey", "value": "secret"},
	}}
}

func tagsOf(item map[string]any) []int {
	var tags []int
	switch value := item["tags"].(type) {
	case []int:
		tags = value
	case []any:
		for _, tag := range value {
			tags = append(tags, int(tag.(float64)))
		}
	}
	return tags
}

func applyTags(item map[string]any, tags []int, apply string) {
	current := tagsOf(item)
	for _, tag := range tags {
		if apply == "add" && !slices.Contains(current, tag) {
			current = append(current, tag)
		}
		if apply == "remove" {
			current = slices.DeleteFunc(current, func(t int) bool { return t == tag })
		}
	}
	item["tags"] = current
}

func (f *fakeIndexerApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	route := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/v3")
	if r.Method != http.MethodGet {
		f.writes = append(f.writes, route)
	}
	byID := func(list []map[string]any, ids []any, tags []any, apply string) {
		var tagIDs []int
		for _, tag := range tags {
			tagIDs = append(tagIDs, int(tag.(float64)))
		}
		for _, item := range list {
			if slices.Contains(ids, any(float64(item["id"].(int)))) {
				applyTags(item, tagIDs, apply)
			}
		}
	}
	switch route {
	case "GET /tag":
		json.NewEncoder(w).Encode(f.tags)
	case "POST /tag":
		body["id"] = len(f.tags) + 10
		f.tags = append(f.tags, body)
		json.NewEncoder(w).Encode(body)
	case "GET /indexer":
		json.NewEncoder(w).Encode(f.indexers)
	case "PUT /indexer/bulk":
		byID(f.indexers, body["ids"].([]any), body["tags"].([]any), body["applyTags"].(string))
		w.WriteHeader(http.StatusAccepted)
	case "GET /autotagging":
		json.NewEncoder(w).Encode(f.rules)
	case "POST /autotagging":
		f.rules = append(f.rules, body)
		json.NewEncoder(w).Encode(body)
	case "GET /" + f.kind:
		json.NewEncoder(w).Encode(f.items)
	case "PUT /" + f.kind + "/editor":
		byID(f.items, body[f.kind+"Ids"].([]any), body["tags"].([]any), body["applyTags"].(string))
		w.WriteHeader(http.StatusAccepted)
	default:
		http.NotFound(w, r)
	}
}

func fakeProwlarrIndexers(indexers ...map[string]any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/indexer" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(indexers)
	})
}

func startIndexerApps(t *testing.T, prowlarr http.Handler, sonarr, radarr *fakeIndexerApp) (*servarr, *servarr, *servarr) {
	t.Helper()
	servers := []*httptest.Server{httptest.NewServer(prowlarr), httptest.NewServer(sonarr), httptest.NewServer(radarr)}
	for _, server := range servers {
		t.Cleanup(server.Close)
	}
	return newServarr("prowlarr", servers[0].URL, "v1", "key", servers[0].Client()),
		newServarr("sonarr", servers[1].URL, "v3", "key", servers[1].Client()),
		newServarr("radarr", servers[2].URL, "v3", "key", servers[2].Client())
}

// The live host on 2026-11-13: Prowlarr IDs 1 and 2 are Nyaa and Anime Tosho,
// 3 to 7 are general indexers added by hand.
var liveProwlarr = fakeProwlarrIndexers(
	map[string]any{"id": 1, "definitionName": "nyaasi"},
	map[string]any{"id": 2, "definitionName": "animetosho-xyz"},
	map[string]any{"id": 3, "definitionName": "thepiratebay"},
	map[string]any{"id": 5, "definitionName": "1337x"},
	map[string]any{"id": 7, "definitionName": "torrentdownloads"},
)

func TestKeepAnimeOnAnimeTrackers(t *testing.T) {
	const animeTagID = 3
	sonarrFake := &fakeIndexerApp{
		kind: "series",
		tags: []map[string]any{{"id": animeTagID, "label": "anime"}},
		indexers: []map[string]any{
			prowlarrIndexer(21, 1), prowlarrIndexer(22, 2), prowlarrIndexer(23, 3), prowlarrIndexer(25, 5, 99),
			// Added in Sonarr itself, not by Prowlarr.
			{"id": 30, "tags": []int{}, "fields": []map[string]any{{"name": "baseUrl", "value": "https://example.org/torznab"}}},
		},
		items: []map[string]any{
			{"id": 1, "tags": []int{}, "seriesType": "standard"},
			{"id": 2, "tags": []int{animeTagID}, "seriesType": "anime"},
			// Anime added by hand in Sonarr, without the tag.
			{"id": 3, "tags": []int{}, "seriesType": "anime"},
		},
	}
	radarrFake := &fakeIndexerApp{
		kind:     "movie",
		tags:     []map[string]any{{"id": 7, "label": "anime"}},
		indexers: []map[string]any{prowlarrIndexer(41, 1), prowlarrIndexer(45, 5)},
		items:    []map[string]any{{"id": 1, "tags": []int{}}, {"id": 2, "tags": []int{7}}},
	}
	prowlarr, sonarr, radarr := startIndexerApps(t, liveProwlarr, sonarrFake, radarrFake)

	var logs []string
	cfg := seerrConfig()
	cfg.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	keepAnimeOnAnimeTrackers(context.Background(), cfg, prowlarr, sonarr, radarr, animeTags{Sonarr: animeTagID, Radarr: 7})
	if len(logs) != 0 {
		t.Fatalf("logs: %v", logs)
	}

	general := sonarrFake.tags[1]
	if general["label"] != "general" {
		t.Fatalf("sonarr tags: %v", sonarrFake.tags)
	}
	generalID := general["id"].(int)
	wantIndexers := map[int][]int{21: nil, 22: nil, 23: {generalID}, 25: {99, generalID}, 30: {generalID}}
	for _, indexer := range sonarrFake.indexers {
		if got := tagsOf(indexer); !slices.Equal(got, wantIndexers[indexer["id"].(int)]) {
			t.Errorf("sonarr indexer %v tags %v, want %v", indexer["id"], got, wantIndexers[indexer["id"].(int)])
		}
	}
	wantSeries := map[int][]int{1: {generalID}, 2: {animeTagID}, 3: nil}
	for _, item := range sonarrFake.items {
		if got := tagsOf(item); !slices.Equal(got, wantSeries[item["id"].(int)]) {
			t.Errorf("series %v tags %v, want %v", item["id"], got, wantSeries[item["id"].(int)])
		}
	}
	if len(sonarrFake.rules) != 1 {
		t.Fatalf("sonarr rules: %v", sonarrFake.rules)
	}
	rule := sonarrFake.rules[0]
	specs := rule["specifications"].([]any)
	if rule["name"] != generalRule || rule["removeTagsAutomatically"] != true || len(specs) != 2 {
		t.Fatalf("sonarr rule: %v", rule)
	}
	for i, want := range []struct {
		implementation string
		value          float64
	}{{"TagSpecification", animeTagID}, {"SeriesTypeSpecification", seriesTypeAnime}} {
		spec := specs[i].(map[string]any)
		field := spec["fields"].([]any)[0].(map[string]any)
		if spec["implementation"] != want.implementation || spec["negate"] != true || spec["required"] != true || field["name"] != "value" || field["value"] != want.value {
			t.Errorf("spec %d: %v", i, spec)
		}
	}

	radarrGeneral := radarrFake.tags[1]["id"].(int)
	if got := tagsOf(radarrFake.indexers[0]); len(got) != 0 {
		t.Errorf("radarr Nyaa tags %v", got)
	}
	if got := tagsOf(radarrFake.indexers[1]); !slices.Equal(got, []int{radarrGeneral}) {
		t.Errorf("radarr 1337x tags %v", got)
	}
	if got := tagsOf(radarrFake.items[0]); !slices.Equal(got, []int{radarrGeneral}) {
		t.Errorf("film tags %v", got)
	}
	if got := tagsOf(radarrFake.items[1]); !slices.Equal(got, []int{7}) {
		t.Errorf("anime film tags %v", got)
	}
	if specs := radarrFake.rules[0]["specifications"].([]any); len(specs) != 1 {
		t.Errorf("radarr rule specs %v", specs)
	}

	// A second run changes nothing.
	sonarrWrites, radarrWrites := len(sonarrFake.writes), len(radarrFake.writes)
	keepAnimeOnAnimeTrackers(context.Background(), cfg, prowlarr, sonarr, radarr, animeTags{Sonarr: animeTagID, Radarr: 7})
	if len(sonarrFake.writes) != sonarrWrites || len(radarrFake.writes) != radarrWrites {
		t.Fatalf("second run wrote: %v, %v", sonarrFake.writes[sonarrWrites:], radarrFake.writes[radarrWrites:])
	}
}

func TestKeepAnimeWithoutTrackersUntagsIndexers(t *testing.T) {
	// The anime trackers are gone, so the general tag must not leave anime
	// without any indexer.
	sonarrFake := &fakeIndexerApp{
		kind:     "series",
		tags:     []map[string]any{{"id": 3, "label": "anime"}, {"id": 4, "label": "general"}},
		indexers: []map[string]any{prowlarrIndexer(23, 3, 4), prowlarrIndexer(25, 5)},
		items:    []map[string]any{{"id": 1, "tags": []int{}, "seriesType": "standard"}},
	}
	radarrFake := &fakeIndexerApp{kind: "movie"}
	prowlarr, sonarr, radarr := startIndexerApps(t, fakeProwlarrIndexers(map[string]any{"id": 3, "definitionName": "thepiratebay"}), sonarrFake, radarrFake)

	var logs []string
	cfg := seerrConfig()
	cfg.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	keepAnimeOnAnimeTrackers(context.Background(), cfg, prowlarr, sonarr, radarr, animeTags{Sonarr: 3})

	for _, indexer := range sonarrFake.indexers {
		if got := tagsOf(indexer); len(got) != 0 {
			t.Errorf("indexer %v tags %v", indexer["id"], got)
		}
	}
	if len(sonarrFake.rules) != 0 || len(radarrFake.writes) != 0 {
		t.Fatalf("rules %v, radarr writes %v", sonarrFake.rules, radarrFake.writes)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "no anime trackers") {
		t.Fatalf("logs: %v", logs)
	}
}

func TestKeepAnimeOnAnimeTrackersOnlyWarns(t *testing.T) {
	broken := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusInternalServerError) })
	prowlarr, sonarr, radarr := startIndexerApps(t, broken, &fakeIndexerApp{kind: "series"}, &fakeIndexerApp{kind: "movie"})

	var logs []string
	cfg := seerrConfig()
	cfg.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	keepAnimeOnAnimeTrackers(context.Background(), cfg, prowlarr, sonarr, radarr, animeTags{Sonarr: 3, Radarr: 7})
	if len(logs) != 1 || !strings.Contains(logs[0], "anime searches every indexer") {
		t.Fatalf("logs: %v", logs)
	}
}
