package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// GamingSystems are the folders under roms/ on the Games share, in the order
// the Gaming page shows them.
var GamingSystems = []struct{ Folder, Label string }{
	{"gb", "Game Boy"},
	{"gbc", "Game Boy Color"},
	{"gba", "Game Boy Advance"},
	{"n64", "Nintendo 64"},
	{"gc", "GameCube"},
}

// gamingSharesRoot is where the host mounts the share bindings.
const gamingSharesRoot = "/mnt/iglo/shares/"

// notGames are files that frontends and scrapers leave next to the games.
var notGames = map[string]bool{"gamelist.xml": true, "media": true, "images": true, "videos": true, "manuals": true}

// maxGames is the most games that one system lists, so a wrong folder can't
// flood the page.
const maxGames = 5000

// coverTypes are the image files that work as your own cover, in the order
// they are tried.
var coverTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp"}

// ErrNoCover means the Games share has no cover of its own for a game.
var ErrNoCover = errors.New("no cover on the Games share")

// GameSystem is one system folder on the Games share with its games.
type GameSystem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Games []Game `json:"games"`
}

// Game is one entry in a system folder: a ROM file, or a folder like an
// extracted disc.
type Game struct {
	// Name is the file name without its extension. Covers match on it.
	Name string `json:"name"`
	// Title is the name without tags like (USA) or [!].
	Title string `json:"title"`
	// Cover is set when the Games share has a cover of its own for the game.
	Cover bool `json:"cover"`
}

// GamingSession is one Moonlight stream on Wolf. Wolf also lists the stream
// keys of each session; they never leave the host.
type GamingSession struct {
	ID       string `json:"id"`
	ClientIP string `json:"clientIP"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FPS      int    `json:"fps"`
}

// wolfSocket is the API socket of Wolf in the gaming container.
const wolfSocket = "/var/run/wolf/wolf.sock"

// wolfSessionID is the form of a Wolf session ID: a number.
var wolfSessionID = regexp.MustCompile(`^[0-9]{1,20}$`)

// ErrNoWolfSession means Wolf has no session with that ID.
var ErrNoWolfSession = errors.New("no such gaming session")

// wolfCall calls the API of Wolf in the gaming container, with curl on its
// socket, and returns the answer. Wolf answers with success false, or an HTTP
// error, when it refuses.
func wolfCall(ctx context.Context, vmid int, method, path string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	// gamingRoot also checks that vmid is the Gaming app.
	if _, err := gamingRoot(ctx, vmid); err != nil {
		return nil, err
	}
	args := []string{"exec", strconv.Itoa(vmid), "--", "curl", "--fail-with-body", "--silent", "--show-error", "--max-time", "8",
		"--unix-socket", wolfSocket, "--request", method, "http://localhost" + path}
	if body != nil {
		args = append(args, "--header", "Content-Type: application/json", "--data-binary", "@-")
	}
	cmd := exec.CommandContext(ctx, "pct", args...)
	cmd.Stdin = strings.NewReader(string(body))
	result, err := cmd.Output()
	if err != nil {
		return nil, errors.New("Wolf did not answer; it may still be starting")
	}
	var answer struct {
		Success bool `json:"success"`
	}
	if json.Unmarshal(result, &answer) != nil || !answer.Success {
		return nil, errors.New("Wolf refused the request")
	}
	return result, nil
}

// gamingSessions lists the Moonlight streams that Wolf runs.
func gamingSessions(ctx context.Context, vmid int) ([]GamingSession, error) {
	result, err := wolfCall(ctx, vmid, http.MethodGet, "/api/v1/sessions", nil)
	if err != nil {
		return nil, err
	}
	return wolfSessions(result)
}

// wolfSessions reads the session list of Wolf, oldest first, because Wolf
// adds a new session at the end. It keeps only what the manager shows.
func wolfSessions(data []byte) ([]GamingSession, error) {
	var answer struct {
		Sessions []struct {
			// Wolf calls the session ID client_id in this list.
			ID       *string `json:"client_id"`
			ClientIP string  `json:"client_ip"`
			Width    int     `json:"video_width"`
			Height   int     `json:"video_height"`
			FPS      int     `json:"video_refresh_rate"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, errors.New("Wolf sent an invalid session list")
	}
	sessions := []GamingSession{}
	for _, session := range answer.Sessions {
		if session.ID == nil || !wolfSessionID.MatchString(*session.ID) {
			continue
		}
		sessions = append(sessions, GamingSession{ID: *session.ID, ClientIP: session.ClientIP, Width: session.Width, Height: session.Height, FPS: session.FPS})
	}
	return sessions, nil
}

// stopGamingSession ends one Moonlight stream. Its emulator stops with it.
func stopGamingSession(ctx context.Context, vmid int, id string) error {
	if !wolfSessionID.MatchString(id) {
		return ErrNoWolfSession
	}
	body, _ := json.Marshal(map[string]string{"session_id": id})
	_, err := wolfCall(ctx, vmid, http.MethodPost, "/api/v1/sessions/stop", body)
	return err
}

// gamingRoot finds the Games share of the gaming container on the host.
func gamingRoot(ctx context.Context, vmid int) (string, error) {
	if vmid < 100 || vmid > 999999999 {
		return "", ErrInvalidContainer
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	config, err := containerConfig(ctx, vmid)
	if err != nil {
		return "", errors.New("could not inspect the gaming container")
	}
	return gamesShare(config)
}

// GamingStats counts the games per system on the Games share of the gaming
// container. It reads the share on the host, so the container may be stopped.
func GamingStats(ctx context.Context, vmid int) (AppStats, error) {
	root, err := gamingRoot(ctx, vmid)
	if err != nil {
		return AppStats{}, err
	}
	figures, err := gamingLibrary(filepath.Join(root, "roms"))
	if err != nil {
		return AppStats{}, err
	}
	return AppStats{Figures: figures, Activity: []Activity{}}, nil
}

// gamesShare finds the host folder that the container sees as /data/games,
// from the output of pct config. It only accepts a share binding.
func gamesShare(config string) (string, error) {
	tagged := false
	share := ""
	for line := range strings.Lines(config) {
		line = strings.TrimSpace(line)
		if tags, ok := strings.CutPrefix(line, "tags: "); ok {
			found := map[string]bool{}
			for _, tag := range strings.Split(tags, ";") {
				found[strings.TrimSpace(tag)] = true
			}
			tagged = found["iglo"] && found[GamingApp]
		}
		key, value, ok := strings.Cut(line, ": ")
		if !ok || !strings.HasPrefix(key, "mp") {
			continue
		}
		options := strings.Split(value, ",")
		if slices.Contains(options[1:], "mp=/data/games") {
			share = options[0]
		}
	}
	if !tagged {
		return "", errors.New("this container is not the Gaming app of Iglo-OS")
	}
	clean := filepath.Clean(share)
	if share == "" || !strings.HasPrefix(clean, gamingSharesRoot) || strings.Contains(strings.TrimPrefix(clean, gamingSharesRoot), "/") {
		return "", errors.New("the gaming container has no Games share binding")
	}
	return clean, nil
}

// gamingLibrary counts the games in each system folder. A missing system
// folder counts 0.
func gamingLibrary(roms string) ([]Figure, error) {
	systems, err := gamingGames(roms, "")
	if err != nil {
		return nil, err
	}
	figures := make([]Figure, 0, len(systems))
	for _, system := range systems {
		figures = append(figures, Figure{Label: system.Label, Value: int64(len(system.Games))})
	}
	return figures, nil
}

// gamingGames lists the games of each system folder, sorted by name. A game
// is a file or a folder, like an extracted disc. With covers set, it also
// marks the games that have a cover of their own in covers/<system>/.
func gamingGames(roms, covers string) ([]GameSystem, error) {
	if info, err := os.Stat(roms); err != nil || !info.IsDir() {
		return nil, errors.New("the Games share has no roms folder, or it is unavailable")
	}
	systems := make([]GameSystem, 0, len(GamingSystems))
	for _, system := range GamingSystems {
		entries, err := os.ReadDir(filepath.Join(roms, system.Folder))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("could not read the " + system.Label + " folder on the Games share")
		}
		own := map[string]bool{}
		if covers != "" {
			images, _ := os.ReadDir(filepath.Join(covers, system.Folder))
			for _, image := range images {
				extension := filepath.Ext(image.Name())
				if coverTypes[strings.ToLower(extension)] != "" && !image.IsDir() {
					own[strings.TrimSuffix(image.Name(), extension)] = true
				}
			}
		}
		games := []Game{}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || notGames[strings.ToLower(name)] {
				continue
			}
			if !entry.IsDir() {
				name = strings.TrimSuffix(name, filepath.Ext(name))
			}
			games = append(games, Game{Name: name, Title: GameTitle(name), Cover: own[name]})
			if len(games) == maxGames {
				break
			}
		}
		systems = append(systems, GameSystem{ID: system.Folder, Label: system.Label, Games: games})
	}
	return systems, nil
}

// GameTitle drops the tags of a ROM name, like (USA, Europe) or [!].
func GameTitle(name string) string {
	if index := strings.IndexAny(name, "(["); index > 0 {
		if title := strings.TrimSpace(name[:index]); title != "" {
			return title
		}
	}
	return name
}

// ValidGame reports whether system and name can name a game, so they are
// safe in a path on the share or in a cover URL.
func ValidGame(system, name string) bool {
	known := slices.ContainsFunc(GamingSystems, func(s struct{ Folder, Label string }) bool { return s.Folder == system })
	return known && name != "" && len(name) <= 255 && !strings.HasPrefix(name, ".") &&
		!strings.ContainsAny(name, "/\\\x00") && name != ".."
}

// gamingCover reads your own cover of a game from covers/<system>/.
func gamingCover(covers, system, name string) ([]byte, string, error) {
	if !ValidGame(system, name) {
		return nil, "", ErrNoCover
	}
	for _, extension := range []string{".png", ".jpg", ".jpeg", ".webp"} {
		file, err := os.Open(filepath.Join(covers, system, name+extension))
		if err != nil {
			continue
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 10<<20))
		if err != nil {
			return nil, "", err
		}
		return data, coverTypes[extension], nil
	}
	return nil, "", ErrNoCover
}

// RegisterGaming adds the game list and the covers of the Games share.
func RegisterGaming(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/gaming/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(checkGamingReady("/"))
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/games", func(w http.ResponseWriter, r *http.Request) {
		vmid, _ := strconv.Atoi(r.PathValue("vmid"))
		root, err := gamingRoot(r.Context(), vmid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		systems, err := gamingGames(filepath.Join(root, "roms"), filepath.Join(root, "covers"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(systems)
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/sessions", func(w http.ResponseWriter, r *http.Request) {
		vmid, _ := strconv.Atoi(r.PathValue("vmid"))
		sessions, err := gamingSessions(r.Context(), vmid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/events", func(w http.ResponseWriter, r *http.Request) {
		vmid, _ := strconv.Atoi(r.PathValue("vmid"))
		// gamingRoot also checks that vmid is the Gaming app.
		if _, err := gamingRoot(r.Context(), vmid); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		relayGuestStream(w, r, vmid, []string{"--unix-socket", wolfSocket, "http://localhost/api/v1/events"}, "sessions", wolfSessionEvent)
	})
	mux.HandleFunc("POST /v1/gaming/{vmid}/sessions/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		vmid, _ := strconv.Atoi(r.PathValue("vmid"))
		if err := stopGamingSession(r.Context(), vmid, r.PathValue("id")); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /v1/gaming/{vmid}/covers/{system}/{name}", func(w http.ResponseWriter, r *http.Request) {
		vmid, _ := strconv.Atoi(r.PathValue("vmid"))
		root, err := gamingRoot(r.Context(), vmid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		data, contentType, err := gamingCover(filepath.Join(root, "covers"), r.PathValue("system"), r.PathValue("name"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
	})
}

func (c *Client) GamingGames(ctx context.Context, vmid int) ([]GameSystem, error) {
	var systems []GameSystem
	err := c.do(ctx, http.MethodGet, "/v1/gaming/"+strconv.Itoa(vmid)+"/games", nil, &systems)
	return systems, err
}

// GamingSessions lists the Moonlight streams of the gaming container, oldest first.
func (c *Client) GamingSessions(ctx context.Context, vmid int) ([]GamingSession, error) {
	var sessions []GamingSession
	err := c.do(ctx, http.MethodGet, "/v1/gaming/"+strconv.Itoa(vmid)+"/sessions", nil, &sessions)
	return sessions, err
}

// StopGamingSession ends one Moonlight stream of the gaming container.
func (c *Client) StopGamingSession(ctx context.Context, vmid int, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/gaming/"+strconv.Itoa(vmid)+"/sessions/"+url.PathEscape(id)+"/stop", nil, nil)
}

// GamingCover returns your own cover of a game, or ErrNoCover.
func (c *Client) GamingCover(ctx context.Context, vmid int, system, name string) ([]byte, string, error) {
	path := fmt.Sprintf("http://agent/v1/gaming/%d/covers/%s/%s", vmid, url.PathEscape(system), url.PathEscape(name))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, "", err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("host agent: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, "", ErrNoCover
	}
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("host agent: %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	return data, response.Header.Get("Content-Type"), err
}

// GamingReadiness says whether the host is prepared for the Gaming app, as
// in docs/gaming.md, before an install would stop on it.
type GamingReadiness struct {
	Ready bool `json:"ready"`
	// Missing names what the host still needs, in plain words.
	Missing []string `json:"missing"`
}

// gamingPrerequisites are what the installer of Gaming checks on the host.
var gamingPrerequisites = []struct {
	path, missing string
	device        bool
}{
	{"/dev/dri/renderD128", "an Intel or AMD GPU (/dev/dri/renderD128)", true},
	{"/dev/uinput", "the uinput module (/dev/uinput)", true},
	{"/dev/uhid", "the uhid module (/dev/uhid)", true},
	{"/etc/udev/rules.d/85-wolf.rules", "the Wolf input rules (/etc/udev/rules.d/85-wolf.rules)", false},
}

// checkGamingReady looks for the prerequisites under root, which is / on the host.
func checkGamingReady(root string) GamingReadiness {
	readiness := GamingReadiness{Missing: []string{}}
	for _, need := range gamingPrerequisites {
		info, err := os.Stat(filepath.Join(root, need.path))
		if err != nil || (need.device && info.Mode()&os.ModeCharDevice == 0) {
			readiness.Missing = append(readiness.Missing, need.missing)
		}
	}
	readiness.Ready = len(readiness.Missing) == 0
	return readiness
}

// GamingReady reads whether the host is prepared for Gaming.
func (c *Client) GamingReady(ctx context.Context) (GamingReadiness, error) {
	var readiness GamingReadiness
	err := c.do(ctx, http.MethodGet, "/v1/gaming/ready", nil, &readiness)
	return readiness, err
}
