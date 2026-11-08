package arr

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

type mediaSizeApp struct {
	api     *servarr
	before  []map[string]any
	updates []map[string]any
}

func managedQualityNames(config []byte, service, root string) (map[string]bool, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(config, &document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 {
		return nil, errors.New("missing Recyclarr config")
	}
	instances := yamlMappingValue(document.Content[0], service)
	if instances == nil || instances.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("missing %s Recyclarr instance", service)
	}
	names := map[string]bool{}
	for i := 1; i < len(instances.Content); i += 2 {
		kind := yamlMappingValue(yamlMappingValue(instances.Content[i], "quality_definition"), "type")
		if kind == nil {
			return nil, fmt.Errorf("missing %s quality definition type", service)
		}
		qualities, err := readQualitySizeGuide(service, kind.Value, root)
		if err != nil {
			return nil, err
		}
		for _, quality := range qualities {
			names[strings.ToLower(quality.Name)] = true
		}
	}
	return names, nil
}

// Sonarr v4.0.20.3014 and Radarr v6.4.4.10685, checked 2026-11-08:
// Sonarr-QualityDefinitionController.cs:32 and :45, and the matching Radarr
// file at :32 and :45, expose GET /api/v3/qualitydefinition and PUT by ID.
// Sonarr-QualityDefinitionResource.cs:10 and :16, and the Radarr file at :10
// and :13, expose quality, title, weight and nullable minSize, maxSize and
// preferredSize. Preserve the rest of each resource when updating sizes.
// Sonarr-QualityDefinitionResourceValidator.cs:10 and the Radarr file at :10
// require non-null values to satisfy min <= preferred <= max. Null upper
// sizes mean unlimited. Both QualityDefinitionService.cs files at :40 clear
// the cache on Update. Individual PUTs avoid UpdateMany's stale cache.
func prepareMediaSizeApps(ctx context.Context, cfg MediaSettingsConfig, config []byte, root string, sizes SizeLimits) ([]mediaSizeApp, error) {
	services := []struct {
		name, endpoint, key string
		limit               int
	}{{"sonarr", cfg.Endpoints.Sonarr, cfg.SonarrAPIKey, sizes.Series}, {"radarr", cfg.Endpoints.Radarr, cfg.RadarrAPIKey, sizes.Films}}
	apps := make([]mediaSizeApp, len(services))
	failures := make([]error, len(services))
	var wg sync.WaitGroup
	for i, service := range services {
		wg.Go(func() {
			apps[i], failures[i] = prepareMediaSizeApp(ctx, cfg, config, root, service.name, service.endpoint, service.key, service.limit)
		})
	}
	wg.Wait()
	return apps, errors.Join(failures...)
}

func prepareMediaSizeApp(ctx context.Context, cfg MediaSettingsConfig, config []byte, root, service, endpoint, key string, limit int) (mediaSizeApp, error) {
	managed, err := managedQualityNames(config, service, root)
	if err != nil {
		return mediaSizeApp{}, err
	}
	app := mediaSizeApp{api: newServarr(service, endpoint, "v3", key, cfg.HTTPClient)}
	if err := app.api.do(ctx, http.MethodGet, "/qualitydefinition", nil, &app.before); err != nil {
		return mediaSizeApp{}, err
	}
	for _, current := range app.before {
		quality, _ := current["quality"].(map[string]any)
		name, _ := quality["name"].(string)
		if managed[strings.ToLower(name)] {
			continue
		}
		defaults, err := defaultQualitySize(service, name)
		if err != nil {
			return mediaSizeApp{}, err
		}
		wanted := maps.Clone(current)
		maximum, preferred, minimum := defaults.Max, defaults.Preferred, defaults.Min
		if limit > 0 {
			maxSize := float64(limit)
			if maximum != nil {
				maxSize = min(*maximum, maxSize)
			}
			preferredSize := maxSize
			if preferred != nil {
				preferredSize = min(*preferred, maxSize)
			}
			maximum, preferred = &maxSize, &preferredSize
			minimum = min(minimum, maxSize/2)
		}
		wanted["minSize"], wanted["maxSize"], wanted["preferredSize"] = minimum, qualitySizeValue(maximum), qualitySizeValue(preferred)
		if !sameQualitySizes(current, wanted) {
			app.updates = append(app.updates, wanted)
		}
	}
	return app, nil
}

func sameQualitySizes(a, b map[string]any) bool {
	for _, field := range []string{"minSize", "maxSize", "preferredSize"} {
		if !reflect.DeepEqual(a[field], b[field]) {
			return false
		}
	}
	return true
}

func putQualitySizes(ctx context.Context, api *servarr, definition map[string]any) error {
	id := jsonInt(definition["id"])
	if id <= 0 {
		return fmt.Errorf("%s quality definition has no valid ID", api.name)
	}
	return api.do(ctx, http.MethodPut, fmt.Sprintf("/qualitydefinition/%d", id), definition, nil)
}

func restoreMediaSizeApps(ctx context.Context, apps []mediaSizeApp) error {
	var failures []error
	for _, app := range apps {
		var current []map[string]any
		if err := app.api.do(ctx, http.MethodGet, "/qualitydefinition", nil, &current); err != nil {
			failures = append(failures, err)
			continue
		}
		for _, before := range app.before {
			for _, definition := range current {
				if jsonInt(before["id"]) != jsonInt(definition["id"]) {
					continue
				}
				// Both pinned QualityDefinitionService.cs files at :37 cache All
				// for five seconds, and UpdateMany at :47 leaves it intact. A
				// failed Recyclarr sync can have written sizes hidden by that
				// cache. Restore the snapshot even if this GET looks unchanged.
				for _, field := range []string{"minSize", "maxSize", "preferredSize"} {
					definition[field] = before[field]
				}
				if err := putQualitySizes(ctx, app.api, definition); err != nil {
					failures = append(failures, err)
				}
			}
		}
	}
	return errors.Join(failures...)
}
