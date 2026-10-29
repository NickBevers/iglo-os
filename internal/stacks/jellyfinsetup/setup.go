// Package jellyfinsetup does the first setup of a new Jellyfin server: the
// startup wizard, the libraries, hardware transcoding and an API key for
// Iglo-OS.
package jellyfinsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"
)

// AppName is the name of the API key that Iglo-OS uses.
const AppName = "Iglo-OS"

// GPUDevice is the render node that the installer passes to the container.
const GPUDevice = "/dev/dri/renderD128"

type Config struct {
	URL           string
	AdminUsername string
	AdminPassword string
	MoviesPath    string
	SeriesPath    string
	// AnimePath and AnimeMoviesPath are optional libraries for anime.
	AnimePath       string
	AnimeMoviesPath string
	// MusicPath and MusicVideosPath are optional libraries on the Music
	// share, where the Music app publishes. HomeVideosPath is the optional
	// library of the home videos that Inbox makes.
	MusicPath       string
	MusicVideosPath string
	HomeVideosPath  string
	// APIKey is a key of Jellyfin, for AddLibraries only. Setup logs in as
	// the admin instead.
	APIKey string
	// GPU turns on VA-API transcoding, for Intel and AMD.
	GPU bool
	// VAInfo is the output of vainfo in the container. It says which codecs
	// the GPU decodes. Without it, only H.264 and HEVC are turned on.
	VAInfo string
	// HuC says the host loaded the HuC firmware of the GPU, which the Intel
	// low-power encoders need for their bitrate control.
	HuC  bool
	Logf func(format string, args ...any)
	// HTTP is replaced in tests.
	HTTP *http.Client
	// Wait is how long to wait for Jellyfin to start, also when it restarts during the setup.
	Wait time.Duration
}

type server struct {
	baseURL string
	token   string
	http    *http.Client
	wait    time.Duration
}

// retryEvery is the pause between two tries while Jellyfin starts.
var retryEvery = 2 * time.Second

// Setup returns an API key for Iglo-OS. It skips the startup wizard when it
// was already done, so it is safe to run again.
func Setup(ctx context.Context, cfg Config) (string, error) {
	jf := newServer(cfg)

	cfg.Logf("Waiting for Jellyfin to start")
	info, err := jf.waitForStart(ctx, jf.wait)
	if err != nil {
		return "", err
	}

	if !info.StartupWizardCompleted {
		cfg.Logf("Finishing the setup wizard")
		if err := jf.wizard(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
			return "", fmt.Errorf("setup wizard: %w", err)
		}
	}

	if err := jf.login(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		return "", fmt.Errorf("log in as %s: %w", cfg.AdminUsername, err)
	}

	cfg.Logf("Adding the libraries")
	if err := jf.ensureLibraries(ctx, cfg); err != nil {
		return "", err
	}

	if cfg.GPU {
		cfg.Logf("Turning on hardware transcoding (VA-API)")
		if err := jf.enableVAAPI(ctx, gpuSupport(cfg.VAInfo), cfg.HuC); err != nil {
			return "", err
		}
	}

	cfg.Logf("Making scrub previews and chapter images at night")
	if err := jf.nightTasks(ctx, cfg.GPU, gpuSupport(cfg.VAInfo)); err != nil {
		return "", err
	}

	return jf.apiKey(ctx)
}

// AddLibraries adds the libraries of cfg that are missing, with the API key
// cfg.APIKey. The update of Jellyfin uses it: it has no admin password.
func AddLibraries(ctx context.Context, cfg Config) error {
	if cfg.APIKey == "" {
		return errors.New("an API key is required")
	}
	jf := newServer(cfg)
	jf.token = cfg.APIKey

	cfg.Logf("Waiting for Jellyfin to start")
	if _, err := jf.waitForStart(ctx, jf.wait); err != nil {
		return err
	}
	cfg.Logf("Adding the libraries that are missing")
	return jf.ensureLibraries(ctx, cfg)
}

func newServer(cfg Config) *server {
	client, wait := cfg.HTTP, cfg.Wait
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if wait == 0 {
		wait = 3 * time.Minute
	}
	return &server{baseURL: strings.TrimRight(cfg.URL, "/"), http: client, wait: wait}
}

// library is a library of Jellyfin. kind is its collection type: Jellyfin
// v12.4 knows movies, tvshows, music, musicvideos, homevideos, boxsets,
// books and mixed (CollectionTypeOptions.cs, checked on 29 October 2026).
type library struct{ name, kind, path string }

// libraries are the libraries of cfg with a path, in the order they are added.
func (cfg Config) libraries() []library {
	all := []library{
		{"Movies", "movies", cfg.MoviesPath},
		{"Series", "tvshows", cfg.SeriesPath},
		{"Anime", "tvshows", cfg.AnimePath},
		{"Anime Movies", "movies", cfg.AnimeMoviesPath},
		{"Music", "music", cfg.MusicPath},
		{"Music Videos", "musicvideos", cfg.MusicVideosPath},
		{"Home Videos", "homevideos", cfg.HomeVideosPath},
	}
	return slices.DeleteFunc(all, func(l library) bool { return l.path == "" })
}

func (s *server) ensureLibraries(ctx context.Context, cfg Config) error {
	for _, library := range cfg.libraries() {
		added, err := s.ensureLibrary(ctx, library)
		if err != nil {
			return err
		}
		if !added {
			cfg.Logf("The library %s is there already", library.name)
		}
	}
	return nil
}

type publicInfo struct {
	StartupWizardCompleted bool `json:"StartupWizardCompleted"`
}

func (s *server) waitForStart(ctx context.Context, wait time.Duration) (publicInfo, error) {
	deadline := time.Now().Add(wait)
	for {
		var info publicInfo
		err := s.do(ctx, http.MethodGet, "/System/Info/Public", nil, &info)
		if err == nil {
			return info, nil
		}
		if time.Now().After(deadline) {
			return publicInfo{}, fmt.Errorf("jellyfin did not start: %w", err)
		}
		select {
		case <-ctx.Done():
			return publicInfo{}, ctx.Err()
		case <-time.After(retryEvery):
		}
	}
}

// wizard does the steps of the first-run wizard of the web page.
func (s *server) wizard(ctx context.Context, username, password string) error {
	steps := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/Startup/Configuration", map[string]string{
			"UICulture": "en-US", "MetadataCountryCode": "US", "PreferredMetadataLanguage": "en",
		}},
		// Getting the user makes Jellyfin create the first user.
		{http.MethodGet, "/Startup/User", nil},
		{http.MethodPost, "/Startup/User", map[string]string{"Name": username, "Password": password}},
		{http.MethodPost, "/Startup/RemoteAccess", map[string]bool{"EnableRemoteAccess": true}},
		{http.MethodPost, "/Startup/Complete", nil},
	}
	for _, step := range steps {
		if err := s.do(ctx, step.method, step.path, step.body, nil); err != nil {
			return err
		}
	}

	return nil
}

func (s *server) login(ctx context.Context, username, password string) error {
	var result struct {
		AccessToken string `json:"AccessToken"`
	}
	body := map[string]string{"Username": username, "Pw": password}
	if err := s.do(ctx, http.MethodPost, "/Users/AuthenticateByName", body, &result); err != nil {
		return err
	}
	if result.AccessToken == "" {
		return errors.New("no access token")
	}
	s.token = result.AccessToken

	return nil
}

type virtualFolder struct {
	Name      string   `json:"Name"`
	Locations []string `json:"Locations"`
}

// ensureLibrary adds a library unless one already uses the path or the
// name, and says whether it added it. A library that is there keeps its
// settings. Jellyfin v12.4 adds a second library of the same name as
// "Music2" (LibraryManager.AddVirtualFolder), so the name counts too.
func (s *server) ensureLibrary(ctx context.Context, library library) (bool, error) {
	var folders []virtualFolder
	if err := s.do(ctx, http.MethodGet, "/Library/VirtualFolders", nil, &folders); err != nil {
		return false, err
	}
	if slices.ContainsFunc(folders, func(folder virtualFolder) bool {
		return slices.Contains(folder.Locations, library.path) || strings.EqualFold(folder.Name, library.name)
	}) {
		return false, nil
	}

	query := url.Values{"name": {library.name}, "collectionType": {library.kind}, "refreshLibrary": {"true"}}
	// Scrub previews (trickplay) and chapter images, made by their night tasks
	// only: during a scan they would start whenever Sonarr imports, maybe
	// while someone watches. Jellyfin v12.4 has all four off by default
	// (LibraryOptions.cs).
	body := map[string]any{"LibraryOptions": map[string]any{
		"PathInfos":                               []map[string]string{{"Path": library.path}},
		"EnableTrickplayImageExtraction":          true,
		"ExtractTrickplayImagesDuringLibraryScan": false,
		"EnableChapterImageExtraction":            true,
		"ExtractChapterImagesDuringLibraryScan":   false,
	}}

	return true, s.do(ctx, http.MethodPost, "/Library/VirtualFolders?"+query.Encode(), body, nil)
}

// gpu is what the GPU can do for Jellyfin, read from vainfo.
type gpu struct {
	// codecs are the codecs it decodes, by their Jellyfin names.
	codecs        []string
	hevc10, vp910 bool
	// intel GPUs tone map HDR to SDR in their video engine (VPP).
	intel bool
	// lowPowerH264 and lowPowerHEVC are the encoders in the fixed-function
	// video engine (VDEnc) of Intel GPUs: faster and lighter on the CPU.
	lowPowerH264, lowPowerHEVC bool
	// jpegEncode is a JPEG encoder, for scrub previews on the GPU.
	jpegEncode bool
}

// vaapiCodecs maps the decode profiles of vainfo to the codec names of Jellyfin.
var vaapiCodecs = []struct{ profile, codec string }{
	{"VAProfileH264", "h264"},
	{"VAProfileHEVCMain", "hevc"},
	{"VAProfileMPEG2", "mpeg2video"},
	{"VAProfileVC1", "vc1"},
	{"VAProfileVP8", "vp8"},
	{"VAProfileVP9", "vp9"},
	{"VAProfileAV1", "av1"},
}

// gpuSupport reads the decode profiles from the output of vainfo. Without
// them it keeps to H.264 and HEVC, which every GPU since about 2015 decodes.
func gpuSupport(vainfo string) gpu {
	support := gpu{intel: strings.Contains(vainfo, "Intel iHD driver")}
	for line := range strings.Lines(vainfo) {
		profile, entrypoint, ok := strings.Cut(line, ":")
		profile, entrypoint = strings.TrimSpace(profile), strings.TrimSpace(entrypoint)
		if ok && entrypoint == "VAEntrypointEncSliceLP" {
			support.lowPowerH264 = support.lowPowerH264 || strings.HasPrefix(profile, "VAProfileH264")
			support.lowPowerHEVC = support.lowPowerHEVC || profile == "VAProfileHEVCMain"
		}
		support.jpegEncode = support.jpegEncode || (profile == "VAProfileJPEGBaseline" && entrypoint == "VAEntrypointEncPicture")
		// VLD is the decode entrypoint. The others encode or process.
		if !ok || entrypoint != "VAEntrypointVLD" {
			continue
		}
		for _, known := range vaapiCodecs {
			if strings.HasPrefix(profile, known.profile) && !slices.Contains(support.codecs, known.codec) {
				support.codecs = append(support.codecs, known.codec)
			}
		}
		support.hevc10 = support.hevc10 || profile == "VAProfileHEVCMain10"
		support.vp910 = support.vp910 || profile == "VAProfileVP9Profile2"
	}
	if len(support.codecs) == 0 {
		support.codecs = []string{"h264", "hevc"}
	}
	return support
}

// enableVAAPI turns on VA-API for the codecs that the GPU decodes, with HDR
// tone mapping on an Intel GPU. The Intel low-power encoders are on when
// vainfo lists them and the host loaded HuC: Jellyfin v12.4 then adds
// "-low_power 1" to h264_vaapi and hevc_vaapi (EncodingHelper.cs).
func (s *server) enableVAAPI(ctx context.Context, support gpu, huc bool) error {
	var encoding map[string]any
	if err := s.do(ctx, http.MethodGet, "/System/Configuration/encoding", nil, &encoding); err != nil {
		return err
	}
	encoding["HardwareAccelerationType"] = "vaapi"
	encoding["VaapiDevice"] = GPUDevice
	encoding["EnableHardwareEncoding"] = true
	encoding["HardwareDecodingCodecs"] = support.codecs
	encoding["EnableDecodingColorDepth10Hevc"] = support.hevc10
	encoding["EnableDecodingColorDepth10Vp9"] = support.vp910
	encoding["EnableVppTonemapping"] = support.intel
	encoding["EnableIntelLowPowerH264HwEncoder"] = support.intel && huc && support.lowPowerH264
	encoding["EnableIntelLowPowerHevcHwEncoder"] = support.intel && huc && support.lowPowerHEVC

	return s.do(ctx, http.MethodPost, "/System/Configuration/encoding", encoding, nil)
}

// The night tasks of Jellyfin v12.4 that make images for every video. By
// default trickplay runs at 03:00 without a limit (TrickplayImagesTask.cs),
// the minute of the Iglo-OS backups, and chapter images at 02:00 for up to
// four hours (ChapterImagesTask.cs). They move to 01:00 and stop at 03:00. A
// large library takes a few nights; each night goes on where the last stopped.
var nightTasks = []string{"RefreshTrickplayImages", "RefreshChapterImages"}

const (
	nightStart   = time.Hour
	nightRuntime = 2 * time.Hour
)

type scheduledTask struct {
	ID  string `json:"Id"`
	Key string `json:"Key"`
}

// nightTasks schedules the image tasks in the night window, and makes the
// scrub previews cheap: keyframes only, on the GPU when there is one.
func (s *server) nightTasks(ctx context.Context, useGPU bool, support gpu) error {
	var tasks []scheduledTask
	if err := s.do(ctx, http.MethodGet, "/ScheduledTasks", nil, &tasks); err != nil {
		return err
	}
	// Ticks of .NET are 100 nanoseconds.
	trigger := []map[string]any{{
		"Type":            "DailyTrigger",
		"TimeOfDayTicks":  int64(nightStart / 100),
		"MaxRuntimeTicks": int64(nightRuntime / 100),
	}}
	for _, key := range nightTasks {
		index := slices.IndexFunc(tasks, func(task scheduledTask) bool { return task.Key == key })
		if index < 0 {
			return fmt.Errorf("jellyfin has no task %s", key)
		}
		if err := s.do(ctx, http.MethodPost, "/ScheduledTasks/"+url.PathEscape(tasks[index].ID)+"/Triggers", trigger, nil); err != nil {
			return err
		}
	}

	// TrickplayOptions in the server configuration (TrickplayOptions.cs).
	// Keyframes only is much faster, with previews a little off their second.
	// VA-API decodes keyframes only on the GPU (MediaEncoder.cs).
	var config map[string]any
	if err := s.do(ctx, http.MethodGet, "/System/Configuration", nil, &config); err != nil {
		return err
	}
	trickplay, _ := config["TrickplayOptions"].(map[string]any)
	if trickplay == nil {
		trickplay = map[string]any{}
	}
	trickplay["EnableKeyFrameOnlyExtraction"] = true
	trickplay["EnableHwAcceleration"] = useGPU
	trickplay["EnableHwEncoding"] = useGPU && support.jpegEncode
	config["TrickplayOptions"] = trickplay

	return s.do(ctx, http.MethodPost, "/System/Configuration", config, nil)
}

// apiKey makes an API key for Iglo-OS, or returns the one it made before.
func (s *server) apiKey(ctx context.Context) (string, error) {
	find := func() (string, error) {
		var keys struct {
			Items []struct {
				AccessToken string `json:"AccessToken"`
				AppName     string `json:"AppName"`
			} `json:"Items"`
		}
		if err := s.do(ctx, http.MethodGet, "/Auth/Keys", nil, &keys); err != nil {
			return "", err
		}
		for _, key := range keys.Items {
			if key.AppName == AppName && key.AccessToken != "" {
				return key.AccessToken, nil
			}
		}
		return "", nil
	}

	if key, err := find(); err != nil || key != "" {
		return key, err
	}
	if err := s.do(ctx, http.MethodPost, "/Auth/Keys?app="+AppName, nil, nil); err != nil {
		return "", err
	}
	key, err := find()
	if err == nil && key == "" {
		err = errors.New("jellyfin made no API key")
	}

	return key, err
}

// do sends a request. While Jellyfin (re)starts, it refuses connections or
// answers 503, and then do tries again until s.wait is over. Jellyfin did
// not handle such a request, so a second try is safe.
func (s *server) do(ctx context.Context, method, path string, body, out any) error {
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return err
		}
	}

	deadline := time.Now().Add(s.wait)
	for {
		err := s.send(ctx, method, path, data, out)
		if !starting(err) || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryEvery):
		}
	}
}

type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string { return e.message }

func starting(err error) bool {
	var status *statusError
	return errors.Is(err, syscall.ECONNREFUSED) || (errors.As(err, &status) && status.status == http.StatusServiceUnavailable)
}

func (s *server) send(ctx context.Context, method, path string, data []byte, out any) error {
	var reader io.Reader
	if data != nil {
		reader = bytes.NewReader(data)
	}

	request, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return err
	}
	auth := `MediaBrowser Client="Iglo-OS", Device="Iglo-OS installer", DeviceId="iglo-installer", Version="1"`
	if s.token != "" {
		auth += `, Token="` + s.token + `"`
	}
	request.Header.Set("Authorization", auth)
	if data != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := s.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return &statusError{
			status:  response.StatusCode,
			message: fmt.Sprintf("%s %s: HTTP %d %s", method, path, response.StatusCode, strings.TrimSpace(string(message))),
		}
	}
	if out == nil {
		return nil
	}

	return json.NewDecoder(response.Body).Decode(out)
}
