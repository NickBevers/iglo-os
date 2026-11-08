package arr

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func readQualitySizeGuide(service, kind string, roots ...string) ([]guideQualitySize, error) {
	var local []guideQualitySize
	if len(roots) > 0 && roots[0] != "" {
		// Discover the cached resource's docs/<service>/quality-size directory.
		// This layout is in Recyclarr v8.8.0's cached source tree fixtures,
		// checked 2026-11-08. The cache root implementation is absent, so search
		// under /config, skip Git objects and reject ambiguous copies.
		err := filepath.WalkDir(roots[0], func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			parent := filepath.Dir(path)
			if !strings.EqualFold(filepath.Ext(path), ".json") || filepath.Base(parent) != "quality-size" ||
				!strings.EqualFold(filepath.Base(filepath.Dir(parent)), service) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var guide struct {
				Type      string             `json:"type"`
				Qualities []guideQualitySize `json:"qualities"`
			}
			if err := json.Unmarshal(data, &guide); err != nil {
				return fmt.Errorf("read local quality guide %s: %w", path, err)
			}
			if guide.Type == kind {
				if len(local) > 0 {
					return fmt.Errorf("multiple local %s quality guides have type %q", service, kind)
				}
				if len(guide.Qualities) == 0 {
					return fmt.Errorf("local %s quality guide %q has no qualities", service, kind)
				}
				local = guide.Qualities
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read Recyclarr local guide: %w", err)
		}
	}
	if len(local) > 0 {
		return local, nil
	}
	data, err := qualitySizeGuides.ReadFile("quality_sizes/trash-" + service + "-quality-size-" + kind + ".json")
	if err != nil {
		return nil, fmt.Errorf("maximum download size: unsupported %s quality definition %q", service, kind)
	}
	var guide struct {
		Qualities []guideQualitySize `json:"qualities"`
	}
	if err := json.Unmarshal(data, &guide); err != nil {
		return nil, err
	}
	return guide.Qualities, nil
}
