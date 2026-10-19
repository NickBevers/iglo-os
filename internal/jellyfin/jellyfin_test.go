package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithThemeKeepsUserCSS(t *testing.T) {
	userCSS := ".myRule { color: pink; }"

	enabled := withTheme(userCSS, true)
	if !strings.HasPrefix(enabled, userCSS) || !strings.Contains(enabled, themeStart) || !strings.HasSuffix(enabled, themeEnd) {
		t.Fatalf("theme not added after user CSS:\n%s", enabled)
	}

	if again := withTheme(enabled, true); strings.Count(again, themeStart) != 1 {
		t.Fatal("enabling twice added the theme twice")
	}

	if disabled := withTheme(enabled, false); disabled != userCSS {
		t.Fatalf("disabling should leave only the user CSS, got %q", disabled)
	}
}

func TestSetThemePostsBrandingWithTheme(t *testing.T) {
	var posted branding
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != `MediaBrowser Token="key"` {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /System/Configuration/branding":
			json.NewEncoder(w).Encode(branding{LoginDisclaimer: "Welcome", CustomCss: ""})
		case "POST /System/Configuration/Branding":
			json.NewDecoder(r.Body).Decode(&posted)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := NewClient(server.URL, "key").SetTheme(context.Background(), true); err != nil {
		t.Fatal(err)
	}

	if posted.LoginDisclaimer != "Welcome" {
		t.Error("other branding settings were lost")
	}
	if !strings.Contains(posted.CustomCss, "--nf-red") {
		t.Error("theme CSS was not posted")
	}
}

func TestNowPlayingShowsSeriesForEpisodes(t *testing.T) {
	session := Session{UserName: "dries", DeviceName: "TV"}
	session.NowPlayingItem = &Item{ID: "episode", SeriesID: "series", Type: "Episode", Name: "Pilot", SeriesName: "Pioneer One", SeasonNumber: 1, EpisodeNumber: 1, RunTimeTicks: 1000}
	session.PlayState.PositionTicks = 250
	session.PlayState.PlayMethod = "Transcode"

	playing := nowPlaying(session)

	if playing.Title != "Pioneer One" || playing.Subtitle != "S1:E1, Pilot" || playing.ItemID != "series" {
		t.Fatalf("unexpected titles: %+v", playing)
	}
	if playing.Progress != 25 || !playing.Transcode {
		t.Fatalf("unexpected progress or transcode: %+v", playing)
	}
}

func TestCountsReadsVideosAndMusic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Items/Counts" {
			http.NotFound(w, r)
			return
		}
		// A part of what Jellyfin answers.
		w.Write([]byte(`{"MovieCount":412,"SeriesCount":38,"EpisodeCount":1290,"ArtistCount":61,"AlbumCount":97,"SongCount":1534,"MusicVideoCount":12}`))
	}))
	defer server.Close()

	counts, err := NewClient(server.URL, "key").Counts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Counts{MovieCount: 412, SeriesCount: 38, EpisodeCount: 1290, AlbumCount: 97, SongCount: 1534, MusicVideoCount: 12}
	if counts != want {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
}
