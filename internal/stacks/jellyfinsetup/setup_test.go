package jellyfinsetup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeJellyfin struct {
	mu        sync.Mutex
	completed bool
	user      map[string]string
	calls     []string
	libraries []map[string]any
	encoding  map[string]any
	config    map[string]any
	triggers  map[string][]map[string]any
	keys      []string
	css       string
}

func (f *fakeJellyfin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Jellyfin (ASP.NET) matches paths without case.
	call := r.Method + " " + r.URL.Path
	if strings.EqualFold(r.URL.Path, "/System/Configuration/Branding") {
		call = r.Method + " /System/Configuration/branding"
	}
	f.calls = append(f.calls, call)
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	json.Unmarshal(raw, &body)

	authorized := strings.Contains(r.Header.Get("Authorization"), `Token="user-token"`) ||
		strings.Contains(r.Header.Get("Authorization"), `Token="api-key"`)
	wizardStep := strings.HasPrefix(r.URL.Path, "/Startup/")
	public := call == "GET /System/Info/Public" || call == "POST /Users/AuthenticateByName"
	if !public && !(wizardStep && !f.completed) && !authorized {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch call {
	case "GET /System/Info/Public":
		json.NewEncoder(w).Encode(map[string]bool{"StartupWizardCompleted": f.completed})
	case "POST /Startup/User":
		f.user = map[string]string{"name": body["Name"].(string), "password": body["Password"].(string)}
	case "POST /Startup/Complete":
		f.completed = true
	case "POST /Users/AuthenticateByName":
		if f.user == nil || body["Username"] != f.user["name"] || body["Pw"] != f.user["password"] {
			http.Error(w, "wrong password", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"AccessToken":"user-token"}`))
	case "GET /Library/VirtualFolders":
		json.NewEncoder(w).Encode(f.libraries)
	case "POST /Library/VirtualFolders":
		options := body["LibraryOptions"].(map[string]any)
		path := options["PathInfos"].([]any)[0].(map[string]any)["Path"]
		f.libraries = append(f.libraries, map[string]any{"Name": r.URL.Query().Get("name"), "CollectionType": r.URL.Query().Get("collectionType"), "Locations": []any{path}, "LibraryOptions": options})
	case "GET /ScheduledTasks":
		// Some of the tasks of Jellyfin v12.4, with made-up IDs.
		w.Write([]byte(`[{"Id":"a1","Key":"RefreshChapterImages"},{"Id":"b2","Key":"RefreshLibrary"},{"Id":"c3","Key":"RefreshTrickplayImages"}]`))
	case "POST /ScheduledTasks/a1/Triggers", "POST /ScheduledTasks/c3/Triggers":
		var triggers []map[string]any
		json.Unmarshal(raw, &triggers)
		if f.triggers == nil {
			f.triggers = map[string][]map[string]any{}
		}
		f.triggers[strings.Split(r.URL.Path, "/")[2]] = triggers
		w.WriteHeader(http.StatusNoContent)
	case "GET /System/Configuration":
		if f.config == nil {
			f.config = map[string]any{"ServerName": "media", "TrickplayOptions": map[string]any{"Interval": 10000, "ProcessThreads": 1}}
		}
		json.NewEncoder(w).Encode(f.config)
	case "POST /System/Configuration":
		f.config = body
	case "GET /System/Configuration/encoding":
		json.NewEncoder(w).Encode(f.encoding)
	case "POST /System/Configuration/encoding":
		f.encoding = body
	case "GET /Auth/Keys":
		items := []map[string]string{}
		for _, app := range f.keys {
			items = append(items, map[string]string{"AppName": app, "AccessToken": "api-key"})
		}
		json.NewEncoder(w).Encode(map[string]any{"Items": items})
	case "POST /Auth/Keys":
		f.keys = append(f.keys, r.URL.Query().Get("app"))
	case "GET /System/Configuration/branding":
		json.NewEncoder(w).Encode(map[string]string{"CustomCss": f.css})
	case "POST /System/Configuration/branding":
		f.css, _ = body["CustomCss"].(string)
	}
}

func testConfig(url string) Config {
	return Config{
		URL: url, AdminUsername: "dries", AdminPassword: "secret",
		MoviesPath: "/data/media/movies", SeriesPath: "/data/media/series",
		GPU: true, Theme: true, Logf: func(string, ...any) {}, Wait: time.Second,
	}
}

func TestSetup(t *testing.T) {
	fake := &fakeJellyfin{encoding: map[string]any{"HardwareAccelerationType": "none", "EncodingThreadCount": -1}}
	server := httptest.NewServer(fake)
	defer server.Close()

	key, err := Setup(context.Background(), testConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if key != "api-key" {
		t.Fatalf("key = %q", key)
	}

	wizard := []string{"POST /Startup/Configuration", "GET /Startup/User", "POST /Startup/User", "POST /Startup/RemoteAccess", "POST /Startup/Complete"}
	last := -1
	for _, step := range wizard {
		index := slices.Index(fake.calls, step)
		if index <= last {
			t.Fatalf("wizard step %q out of order in %v", step, fake.calls)
		}
		last = index
	}
	if fake.user["name"] != "dries" {
		t.Fatalf("user = %v", fake.user)
	}
	if len(fake.libraries) != 2 || fake.libraries[1]["CollectionType"] != "tvshows" || fake.libraries[1]["Name"] != "Series" {
		t.Fatalf("libraries = %v", fake.libraries)
	}
	if fake.encoding["HardwareAccelerationType"] != "vaapi" || fake.encoding["VaapiDevice"] != GPUDevice || fake.encoding["EncodingThreadCount"] != float64(-1) {
		t.Fatalf("encoding = %v", fake.encoding)
	}
	if !strings.Contains(fake.css, "iglo-theme:start") {
		t.Fatal("the theme was not turned on")
	}

	// A second run skips the wizard and makes no second key or library.
	calls := len(fake.calls)
	if _, err := Setup(context.Background(), testConfig(server.URL)); err != nil {
		t.Fatal(err)
	}
	for _, call := range fake.calls[calls:] {
		if strings.HasPrefix(call, "POST /Startup/") || call == "POST /Auth/Keys" || call == "POST /Library/VirtualFolders" {
			t.Fatalf("second run called %s", call)
		}
	}
}

func TestSetupWithoutGPUAndTheme(t *testing.T) {
	fake := &fakeJellyfin{encoding: map[string]any{"HardwareAccelerationType": "none"}}
	server := httptest.NewServer(fake)
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.GPU, cfg.Theme = false, false
	if _, err := Setup(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if fake.encoding["HardwareAccelerationType"] != "none" || fake.css != "" {
		t.Fatalf("encoding = %v, css = %q", fake.encoding, fake.css)
	}
}

func TestSetupWrongPasswordOnDoneWizard(t *testing.T) {
	fake := &fakeJellyfin{completed: true, user: map[string]string{"name": "dries", "password": "other"}}
	server := httptest.NewServer(fake)
	defer server.Close()

	if _, err := Setup(context.Background(), testConfig(server.URL)); err == nil || !strings.Contains(err.Error(), "log in as dries") {
		t.Fatalf("err = %v", err)
	}
}

// restartingTransport plays a Jellyfin that restarts right after it first
// answers: it refuses connections, then answers 503, then works.
type restartingTransport struct {
	mu      sync.Mutex
	answers int
	refused int
	busy    int
}

func (t *restartingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.answers > 0 {
		switch {
		case t.refused > 0:
			t.refused--
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
		case t.busy > 0:
			t.busy--
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("starting")), Request: request}, nil
		}
	}
	t.answers++

	return http.DefaultTransport.RoundTrip(request)
}

func TestSetupWaitsWhileJellyfinRestarts(t *testing.T) {
	defer func(previous time.Duration) { retryEvery = previous }(retryEvery)
	retryEvery = 10 * time.Millisecond

	fake := &fakeJellyfin{encoding: map[string]any{}}
	server := httptest.NewServer(fake)
	defer server.Close()

	config := testConfig(server.URL)
	config.HTTP = &http.Client{Transport: &restartingTransport{refused: 3, busy: 2}}
	if _, err := Setup(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if !fake.completed {
		t.Fatal("the wizard was not finished")
	}

	// Jellyfin that stays down still fails, after the wait.
	config.HTTP = &http.Client{Transport: &restartingTransport{refused: 1000}}
	config.Wait = 50 * time.Millisecond
	fake.completed = false
	_, err := Setup(context.Background(), config)
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("got %v, want connection refused", err)
	}
}

func TestImagesAreMadeAtNight(t *testing.T) {
	for _, useGPU := range []bool{true, false} {
		fake := &fakeJellyfin{encoding: map[string]any{"HardwareAccelerationType": "none"}}
		server := httptest.NewServer(fake)
		cfg := testConfig(server.URL)
		cfg.GPU = useGPU
		cfg.VAInfo = "vainfo: Driver version: Intel iHD driver\n      VAProfileJPEGBaseline : VAEntrypointEncPicture\n"
		if _, err := Setup(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		server.Close()

		// 01:00 for two hours, in ticks of 100 nanoseconds.
		want := []map[string]any{{"Type": "DailyTrigger", "TimeOfDayTicks": float64(36_000_000_000), "MaxRuntimeTicks": float64(72_000_000_000)}}
		if len(fake.triggers) != 2 || !reflect.DeepEqual(fake.triggers["a1"], want) || !reflect.DeepEqual(fake.triggers["c3"], want) {
			t.Fatalf("triggers = %v", fake.triggers)
		}
		trickplay := fake.config["TrickplayOptions"].(map[string]any)
		if trickplay["EnableKeyFrameOnlyExtraction"] != true || trickplay["EnableHwAcceleration"] != useGPU ||
			trickplay["EnableHwEncoding"] != useGPU || trickplay["Interval"] != float64(10000) || fake.config["ServerName"] != "media" {
			t.Fatalf("gpu %v: config = %v", useGPU, fake.config)
		}
		options := fake.libraries[0]["LibraryOptions"].(map[string]any)
		if options["EnableTrickplayImageExtraction"] != true || options["ExtractTrickplayImagesDuringLibraryScan"] != false ||
			options["EnableChapterImageExtraction"] != true || options["ExtractChapterImagesDuringLibraryScan"] != false {
			t.Fatalf("library options = %v", options)
		}
	}
}

func TestGPUSupportReadsVainfo(t *testing.T) {
	// A shortened vainfo of an Intel UHD 770.
	uhd770 := `libva info: VA-API version 1.22.0
vainfo: Driver version: Intel iHD driver for Intel(R) Gen Graphics - 25.2.3 ()
vainfo: Supported profile and entrypoints
      VAProfileNone                   :	VAEntrypointVideoProc
      VAProfileMPEG2Simple            :	VAEntrypointVLD
      VAProfileH264Main               :	VAEntrypointVLD
      VAProfileH264Main               :	VAEntrypointEncSliceLP
      VAProfileVC1Advanced            :	VAEntrypointVLD
      VAProfileVP8Version0_3          :	VAEntrypointVLD
      VAProfileHEVCMain               :	VAEntrypointVLD
      VAProfileHEVCMain               :	VAEntrypointEncSliceLP
      VAProfileHEVCMain10             :	VAEntrypointVLD
      VAProfileVP9Profile0            :	VAEntrypointVLD
      VAProfileVP9Profile2            :	VAEntrypointVLD
      VAProfileAV1Profile0            :	VAEntrypointVLD
`
	got := gpuSupport(uhd770)
	want := []string{"mpeg2video", "h264", "vc1", "vp8", "hevc", "vp9", "av1"}
	if !slices.Equal(got.codecs, want) || !got.hevc10 || !got.vp910 || !got.intel || !got.lowPowerH264 || !got.lowPowerHEVC {
		t.Fatalf("uhd 770 = %+v", got)
	}

	// An older GPU only encodes HEVC, so it decodes H.264 alone.
	older := "vainfo: Driver version: Mesa Gallium driver\n      VAProfileH264High : VAEntrypointVLD\n      VAProfileHEVCMain : VAEntrypointEncSlice\n"
	if got := gpuSupport(older); !slices.Equal(got.codecs, []string{"h264"}) || got.hevc10 || got.intel || got.lowPowerHEVC {
		t.Fatalf("older = %+v", got)
	}

	// Without vainfo, the safe pair.
	if got := gpuSupport(""); !slices.Equal(got.codecs, []string{"h264", "hevc"}) || got.intel {
		t.Fatalf("empty = %+v", got)
	}
}

func TestLowPowerEncodersNeedHuC(t *testing.T) {
	vainfo := "vainfo: Driver version: Intel iHD driver for Intel(R) Gen Graphics\n" +
		"      VAProfileH264Main : VAEntrypointVLD\n      VAProfileH264Main : VAEntrypointEncSliceLP\n" +
		"      VAProfileHEVCMain : VAEntrypointVLD\n      VAProfileHEVCMain : VAEntrypointEncSliceLP\n"
	for _, huc := range []bool{true, false} {
		fake := &fakeJellyfin{encoding: map[string]any{"HardwareAccelerationType": "none"}}
		server := httptest.NewServer(fake)
		cfg := testConfig(server.URL)
		cfg.VAInfo, cfg.HuC = vainfo, huc
		if _, err := Setup(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		server.Close()
		if fake.encoding["EnableIntelLowPowerH264HwEncoder"] != huc || fake.encoding["EnableIntelLowPowerHevcHwEncoder"] != huc {
			t.Fatalf("huc %v: encoding = %v", huc, fake.encoding)
		}
	}
}

func TestSetupAddsAnimeLibraries(t *testing.T) {
	fake := &fakeJellyfin{encoding: map[string]any{"HardwareAccelerationType": "none", "EncodingThreadCount": -1}}
	server := httptest.NewServer(fake)
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.AnimePath, cfg.AnimeMoviesPath = "/data/media/anime", "/data/media/anime-movies"
	if _, err := Setup(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if len(fake.libraries) != 4 || fake.libraries[2]["Name"] != "Anime" || fake.libraries[2]["CollectionType"] != "tvshows" ||
		fake.libraries[3]["Name"] != "Anime Movies" || fake.libraries[3]["CollectionType"] != "movies" {
		t.Fatalf("libraries = %v", fake.libraries)
	}
}

func TestSetupAddsMusicAndHomeVideosLibraries(t *testing.T) {
	fake := &fakeJellyfin{encoding: map[string]any{"HardwareAccelerationType": "none"}}
	server := httptest.NewServer(fake)
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.MusicPath, cfg.MusicVideosPath, cfg.HomeVideosPath = "/data/music/audio", "/data/music/videos", "/data/media/home-videos"
	if _, err := Setup(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	want := [][2]string{{"Movies", "movies"}, {"Series", "tvshows"}, {"Music", "music"}, {"Music Videos", "musicvideos"}, {"Home Videos", "homevideos"}}
	if len(fake.libraries) != len(want) {
		t.Fatalf("libraries = %v", fake.libraries)
	}
	for i, library := range want {
		if fake.libraries[i]["Name"] != library[0] || fake.libraries[i]["CollectionType"] != library[1] {
			t.Fatalf("library %d = %v, want %v", i, fake.libraries[i], library)
		}
	}
}

func TestAddLibrariesWithAnAPIKey(t *testing.T) {
	// A Music library of your own at another folder, and a library of your
	// own at the folder of Inbox.
	ownOptions := map[string]any{"EnableRealtimeMonitor": false}
	fake := &fakeJellyfin{completed: true, libraries: []map[string]any{
		{"Name": "music", "CollectionType": "music", "Locations": []any{"/mnt/songs"}, "LibraryOptions": ownOptions},
		{"Name": "Family", "CollectionType": "homevideos", "Locations": []any{"/data/media/home-videos"}, "LibraryOptions": ownOptions},
	}}
	server := httptest.NewServer(fake)
	defer server.Close()

	cfg := Config{
		URL: server.URL, APIKey: "api-key", Logf: func(string, ...any) {}, Wait: time.Second,
		MusicPath: "/data/music/audio", MusicVideosPath: "/data/music/videos", HomeVideosPath: "/data/media/home-videos",
	}
	if err := AddLibraries(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	// Only Music Videos is new. The others keep their names, folders and settings.
	if len(fake.libraries) != 3 || fake.libraries[2]["Name"] != "Music Videos" || fake.libraries[2]["CollectionType"] != "musicvideos" {
		t.Fatalf("libraries = %v", fake.libraries)
	}
	if !reflect.DeepEqual(fake.libraries[0]["LibraryOptions"], ownOptions) || !reflect.DeepEqual(fake.libraries[1]["Locations"], []any{"/data/media/home-videos"}) {
		t.Fatalf("an existing library changed: %v", fake.libraries)
	}
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "POST /Startup/") || call == "POST /Users/AuthenticateByName" || strings.HasPrefix(call, "POST /System/") {
			t.Fatalf("the update called %s", call)
		}
	}

	// Again: nothing to add.
	calls := len(fake.calls)
	if err := AddLibraries(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(fake.calls[calls:], "POST /Library/VirtualFolders") {
		t.Fatalf("second run added a library: %v", fake.calls[calls:])
	}
}

func TestAddLibrariesWithAWrongKey(t *testing.T) {
	fake := &fakeJellyfin{completed: true}
	server := httptest.NewServer(fake)
	defer server.Close()

	cfg := Config{URL: server.URL, APIKey: "other-key", MusicPath: "/data/music/audio", Logf: func(string, ...any) {}, Wait: time.Second}
	if err := AddLibraries(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
	cfg.APIKey = ""
	if err := AddLibraries(context.Background(), cfg); err == nil {
		t.Fatal("no error without a key")
	}
}
