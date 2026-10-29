package arr

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// flareSolverrTag is the tag that sends an indexer through FlareSolverr.
const flareSolverrTag = "flaresolverr"

// SetFlareSolverr turns FlareSolverr on or off for a stack that runs, in
// Prowlarr and in the .env at envPath. It needs no password: Prowlarr's API
// key is in .env. The installer starts the service before it turns it on,
// and stops it after it turns it off.
//
// On adds the FlareSolverr indexer proxy. Off deletes the proxy that points
// at the stack's FlareSolverr. The tag stays, and so do the indexers that
// have it: they use FlareSolverr again once it is back on.
//
// Checked against Prowlarr v2.7.0.5723 on 29 October 2026: POST and DELETE
// /api/v1/indexerProxy, and a POST without forceSave tests the proxy first.
// Prowlarr's housekeeping only removes a tag that nothing uses.
func SetFlareSolverr(ctx context.Context, on bool, envPath, prowlarrURL string, logf func(string, ...any)) error {
	data, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}
	apiKey := parseEnv(string(data))["PROWLARR_API_KEY"]
	if apiKey == "" {
		return fmt.Errorf("PROWLARR_API_KEY is missing in %s", envPath)
	}

	prowlarr := newServarr("prowlarr", prowlarrURL, "v1", apiKey, &http.Client{Timeout: 30 * time.Second})
	logf("Waiting for Prowlarr")
	if err := waitFor(ctx, prowlarr.ready); err != nil {
		return err
	}

	if on {
		logf("Adding FlareSolverr to Prowlarr")
		// Prowlarr tests the proxy before it saves it, and FlareSolverr
		// takes a moment to start.
		err = waitFor(ctx, func(ctx context.Context) error { return addFlareSolverr(ctx, prowlarr) })
	} else {
		logf("Removing FlareSolverr from Prowlarr")
		err = removeFlareSolverr(ctx, prowlarr)
	}
	if err != nil {
		return err
	}

	return writeFileAtomic(envPath, []byte(flareSolverrEnv(string(data), on)), 0o600)
}

// addFlareSolverr adds the FlareSolverr proxy to Prowlarr, or updates it.
func addFlareSolverr(ctx context.Context, prowlarr *servarr) error {
	// Prowlarr only sends indexers with this tag through FlareSolverr.
	tag, err := prowlarr.ensureTag(ctx, flareSolverrTag)
	if err != nil {
		return err
	}

	return prowlarr.ensure(ctx, provider{
		resource:       "indexerProxy",
		implementation: "FlareSolverr",
		name:           "FlareSolverr",
		fields:         map[string]any{"host": flaresolverrURL},
		settings:       map[string]any{"tags": []int{tag}},
	})
}

// removeFlareSolverr deletes the FlareSolverr proxies that point at the
// stack's FlareSolverr. A proxy for another FlareSolverr stays.
func removeFlareSolverr(ctx context.Context, prowlarr *servarr) error {
	var proxies []struct {
		ID             int    `json:"id"`
		Implementation string `json:"implementation"`
		Fields         []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"fields"`
	}
	if err := prowlarr.do(ctx, http.MethodGet, "/indexerProxy", nil, &proxies); err != nil {
		return err
	}

	for _, proxy := range proxies {
		if proxy.Implementation != "FlareSolverr" {
			continue
		}
		for _, field := range proxy.Fields {
			host, _ := field.Value.(string)
			if field.Name == "host" && strings.TrimRight(host, "/") == strings.TrimRight(flaresolverrURL, "/") {
				if err := prowlarr.do(ctx, http.MethodDelete, fmt.Sprintf("/indexerProxy/%d", proxy.ID), nil, nil); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// flareSolverrEnv is .env with FLARESOLVERR, and flaresolverr in or out of
// COMPOSE_PROFILES. Other profiles stay.
func flareSolverrEnv(data string, on bool) string {
	var lines, profiles []string
	for line := range strings.Lines(data) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch key {
		case "FLARESOLVERR":
			continue
		case "COMPOSE_PROFILES":
			for profile := range strings.SplitSeq(value, ",") {
				if profile = strings.TrimSpace(profile); profile != "" && profile != "flaresolverr" {
					profiles = append(profiles, profile)
				}
			}
			continue
		}
		lines = append(lines, strings.TrimRight(line, "\n"))
	}

	answer := "n"
	if on {
		answer = "y"
		profiles = append(profiles, "flaresolverr")
	}
	lines = append(lines, "FLARESOLVERR="+answer, "COMPOSE_PROFILES="+strings.Join(profiles, ","))

	return strings.Join(lines, "\n") + "\n"
}
