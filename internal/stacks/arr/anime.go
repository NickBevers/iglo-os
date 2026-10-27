package arr

import (
	"context"
	"net/http"
	"slices"
)

// animeTag marks anime in Sonarr and Radarr. Seerr puts it on the series and
// films it requests as anime, so the delay profile and Bazarr can tell anime
// apart.
const animeTag = "anime"

// A new anime episode lands from several web rippers within minutes. Sonarr
// waits 30 minutes, sees them all and grabs the best one once, instead of
// grabbing the first and upgrading right after. A release in TRaSH's anime web
// tier 1 to 3 (400 points or more) is grabbed without waiting. Checked against
// Sonarr v4.0.20 and TRaSH Guides 6ac5972 on 27 October 2026.
const (
	animeDelayMinutes     = 30
	animeDelayBypassScore = 400
)

// animeTags are the IDs of the anime tag in Sonarr and Radarr. Zero means the
// tag could not be made.
type animeTags struct{ Sonarr, Radarr int }

// configureAnime makes the anime tag in Sonarr and Radarr, and the delay
// profile for it in Sonarr. A failure only warns: anime then works like any
// other series or film.
func configureAnime(ctx context.Context, cfg Config, sonarr, radarr *servarr) animeTags {
	var tags animeTags
	var err error
	if tags.Radarr, err = radarr.ensureTag(ctx, animeTag); err != nil {
		cfg.Logf("Could not add the anime tag to Radarr: %v", err)
		tags.Radarr = 0
	}
	if tags.Sonarr, err = sonarr.ensureTag(ctx, animeTag); err != nil {
		cfg.Logf("Could not add the anime tag to Sonarr: %v", err)
		return animeTags{Radarr: tags.Radarr}
	}
	if err := ensureAnimeDelay(ctx, sonarr, tags.Sonarr); err != nil {
		cfg.Logf("Could not add the anime delay profile to Sonarr: %v", err)
	}

	return tags
}

// ensureAnimeDelay adds the delay profile for the anime tag, once.
func ensureAnimeDelay(ctx context.Context, sonarr *servarr, tag int) error {
	var profiles []struct {
		Tags []int `json:"tags"`
	}
	if err := sonarr.do(ctx, http.MethodGet, "/delayprofile", nil, &profiles); err != nil {
		return err
	}
	for _, profile := range profiles {
		if slices.Contains(profile.Tags, tag) {
			return nil
		}
	}

	profile := map[string]any{
		"enableUsenet": true, "enableTorrent": true, "preferredProtocol": "torrent",
		"usenetDelay": 0, "torrentDelay": animeDelayMinutes,
		"bypassIfHighestQuality": true, "bypassIfAboveCustomFormatScore": true,
		"minimumCustomFormatScore": animeDelayBypassScore,
		"tags":                     []int{tag},
	}

	return sonarr.do(ctx, http.MethodPost, "/delayprofile", profile, nil)
}
