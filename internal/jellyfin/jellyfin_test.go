package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

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

func TestStatusKeepsTheLibraryForAWhile(t *testing.T) {
	calls := map[string]int{}
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/System/Info":
			w.Write([]byte(`{"ServerName":"jellyfin","Version":"10.11.0"}`))
		case "/Items/Counts":
			w.Write([]byte(`{"MovieCount":412}`))
		case "/Items":
			w.Write([]byte(`{"Items":[]}`))
		case "/Sessions":
			w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service, err := NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service.settings = Settings{URL: server.URL, APIKey: "key"}

	for range 3 {
		if view := service.Status(context.Background()); view.Error != "" || view.Movies != 412 {
			t.Fatalf("view = %+v", view)
		}
	}
	// Info and sessions every time, the library once.
	if calls["/System/Info"] != 3 || calls["/Sessions"] != 3 || calls["/Items/Counts"] != 1 {
		t.Fatalf("calls = %v", calls)
	}
	// Saving the settings asks for the library again.
	if err := service.SaveSettings(context.Background(), Settings{URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	service.Status(context.Background())
	if calls["/Items/Counts"] != 2 {
		t.Fatalf("calls = %v", calls)
	}
}
