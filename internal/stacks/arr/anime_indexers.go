package arr

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
)

// generalTag marks the series and films that aren't anime, and the indexers
// that serve only them. Sonarr and Radarr use an indexer with tags only for
// items that share a tag, and an indexer without tags for everything
// (ReleaseSearchService.cs in Sonarr v4.0.20.3014 and Radarr v6.4.4.10685,
// checked 2026-11-13). So the anime trackers stay untagged, every other
// indexer gets this tag, and anime, which lacks it, searches only the anime
// trackers. General indexers like 1337x list a TV/Anime category, so
// categories alone don't keep anime off them.
const generalTag = "general"

// generalRule is the name of the auto tagging rule that puts generalTag on
// everything that isn't anime, when it is added and on each refresh. Sonarr
// applies it before the first search of a new series (RefreshSeriesService).
const generalRule = "Not anime (Iglo-OS)"

// seriesTypeAnime is SeriesTypes.Anime in Sonarr v4.0.20.3014.
const seriesTypeAnime = 2

// prowlarrIndexerID reads the Prowlarr indexer ID from the base URL that
// Prowlarr gives an indexer it syncs, like http://gluetun:9696/2/.
var prowlarrIndexerID = regexp.MustCompile(`/(\d+)/?$`)

type taggedIndexer struct {
	ID     int   `json:"id"`
	Tags   []int `json:"tags"`
	Fields []struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	} `json:"fields"`
}

func (i taggedIndexer) prowlarrID() int {
	for _, field := range i.Fields {
		if field.Name != "baseUrl" {
			continue
		}
		url, _ := field.Value.(string)
		if match := prowlarrIndexerID.FindStringSubmatch(url); match != nil {
			id, _ := strconv.Atoi(match[1])
			return id
		}
	}
	return 0
}

// animeTrackerIDs are the Prowlarr IDs of the anime trackers.
func animeTrackerIDs(ctx context.Context, prowlarr *servarr) (map[int]bool, error) {
	var indexers []struct {
		ID             int    `json:"id"`
		DefinitionName string `json:"definitionName"`
	}
	if err := prowlarr.do(ctx, http.MethodGet, "/indexer", nil, &indexers); err != nil {
		return nil, err
	}
	ids := map[int]bool{}
	for _, indexer := range indexers {
		if slices.ContainsFunc(animeIndexers, func(a struct{ definition, name string }) bool { return a.definition == indexer.DefinitionName }) {
			ids[indexer.ID] = true
		}
	}
	return ids, nil
}

// keepAnimeOnAnimeTrackers makes anime search only the anime trackers, in
// Sonarr and Radarr. It runs on each update, so an indexer added in Prowlarr
// since then gets the tag too. Without anime trackers in an app it takes the
// tag off the indexers again, so anime keeps every indexer instead of none.
// A failure only warns: anime then searches every indexer, as before.
func keepAnimeOnAnimeTrackers(ctx context.Context, cfg Config, prowlarr, sonarr, radarr *servarr, tags animeTags) {
	trackers, err := animeTrackerIDs(ctx, prowlarr)
	if err != nil {
		cfg.Logf("Could not read the indexers of Prowlarr, anime searches every indexer: %v", err)
		return
	}
	apps := []struct {
		app   *servarr
		kind  string
		anime int
	}{{sonarr, "series", tags.Sonarr}, {radarr, "movie", tags.Radarr}}
	for _, app := range apps {
		if app.anime == 0 {
			continue
		}
		if err := tagGeneralIndexers(ctx, cfg, app.app, app.kind, app.anime, trackers); err != nil {
			cfg.Logf("Could not keep anime on the anime trackers in %s, anime searches every indexer: %v", app.app.name, err)
		}
	}
}

func tagGeneralIndexers(ctx context.Context, cfg Config, app *servarr, kind string, anime int, trackers map[int]bool) error {
	general, err := app.ensureTag(ctx, generalTag)
	if err != nil {
		return err
	}
	var indexers []taggedIndexer
	if err := app.do(ctx, http.MethodGet, "/indexer", nil, &indexers); err != nil {
		return err
	}

	var add, remove []int
	hasTracker := slices.ContainsFunc(indexers, func(i taggedIndexer) bool { return trackers[i.prowlarrID()] })
	for _, indexer := range indexers {
		tagged := slices.Contains(indexer.Tags, general)
		isGeneral := hasTracker && !trackers[indexer.prowlarrID()]
		switch {
		case isGeneral && !tagged:
			add = append(add, indexer.ID)
		case !isGeneral && tagged:
			remove = append(remove, indexer.ID)
		}
	}
	// The bulk edit changes only the tags, and doesn't test each indexer.
	for _, change := range []struct {
		ids   []int
		apply string
	}{{add, "add"}, {remove, "remove"}} {
		if len(change.ids) == 0 {
			continue
		}
		body := map[string]any{"ids": change.ids, "tags": []int{general}, "applyTags": change.apply}
		if err := app.do(ctx, http.MethodPut, "/indexer/bulk", body, nil); err != nil {
			return err
		}
	}
	if !hasTracker {
		if len(remove) > 0 {
			cfg.Logf("%s has no anime trackers, so anime searches every indexer", app.name)
		}
		return nil
	}

	if err := ensureGeneralRule(ctx, app, kind, general, anime); err != nil {
		return err
	}
	return tagGeneralItems(ctx, app, kind, general, anime)
}

// ensureGeneralRule adds the auto tagging rule once. Every condition must
// hold: the item lacks the anime tag, and in Sonarr it isn't of the anime
// type either. The rule takes the tag off again when an item becomes anime.
func ensureGeneralRule(ctx context.Context, app *servarr, kind string, general, anime int) error {
	var rules []struct {
		Name string `json:"name"`
	}
	if err := app.do(ctx, http.MethodGet, "/autotagging", nil, &rules); err != nil {
		return err
	}
	if slices.ContainsFunc(rules, func(rule struct {
		Name string `json:"name"`
	}) bool {
		return rule.Name == generalRule
	}) {
		return nil
	}

	specifications := []map[string]any{notSpecification("Not tagged anime", "TagSpecification", anime)}
	if kind == "series" {
		specifications = append(specifications, notSpecification("Not of the anime type", "SeriesTypeSpecification", seriesTypeAnime))
	}
	rule := map[string]any{
		"name": generalRule, "removeTagsAutomatically": true, "tags": []int{general},
		"specifications": specifications,
	}
	return app.do(ctx, http.MethodPost, "/autotagging", rule, nil)
}

func notSpecification(name, implementation string, value int) map[string]any {
	return map[string]any{
		"name": name, "implementation": implementation, "negate": true, "required": true,
		"fields": []map[string]any{{"name": "value", "value": value}},
	}
}

// tagGeneralItems tags the series or films that are there already. The rule
// only acts on an add or a refresh, and Sonarr refreshes every 12 hours.
func tagGeneralItems(ctx context.Context, app *servarr, kind string, general, anime int) error {
	var items []struct {
		ID         int    `json:"id"`
		Tags       []int  `json:"tags"`
		SeriesType string `json:"seriesType"`
	}
	if err := app.do(ctx, http.MethodGet, "/"+kind, nil, &items); err != nil {
		return err
	}
	var add, remove []int
	for _, item := range items {
		isAnime := slices.Contains(item.Tags, anime) || item.SeriesType == "anime"
		tagged := slices.Contains(item.Tags, general)
		switch {
		case !isAnime && !tagged:
			add = append(add, item.ID)
		case isAnime && tagged:
			remove = append(remove, item.ID)
		}
	}
	for _, change := range []struct {
		ids   []int
		apply string
	}{{add, "add"}, {remove, "remove"}} {
		if len(change.ids) == 0 {
			continue
		}
		body := map[string]any{kind + "Ids": change.ids, "tags": []int{general}, "applyTags": change.apply}
		if err := app.do(ctx, http.MethodPut, fmt.Sprintf("/%s/editor", kind), body, nil); err != nil {
			return err
		}
	}
	return nil
}
