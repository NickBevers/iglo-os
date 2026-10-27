package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGamesShareNeedsTagsAndABinding(t *testing.T) {
	config := "arch: amd64\nmp0: /mnt/iglo/shares/games,mp=/data/games,backup=0,ro=1\ntags: iglo;gaming\n"
	if share, err := gamesShare(config); err != nil || share != "/mnt/iglo/shares/games" {
		t.Fatalf("share = %q, %v", share, err)
	}
	for name, config := range map[string]string{
		"no tags":      "mp0: /mnt/iglo/shares/games,mp=/data/games\n",
		"other app":    "mp0: /mnt/iglo/shares/games,mp=/data/games\ntags: iglo;music\n",
		"no share":     "tags: iglo;gaming\n",
		"host folder":  "mp0: /srv/games,mp=/data/games\ntags: iglo;gaming\n",
		"escapes root": "mp0: /mnt/iglo/shares/../../etc,mp=/data/games\ntags: iglo;gaming\n",
		"nested":       "mp0: /mnt/iglo/shares/games/roms,mp=/data/games\ntags: iglo;gaming\n",
	} {
		if _, err := gamesShare(config); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestGamingLibraryCountsGamesPerSystem(t *testing.T) {
	roms := filepath.Join(t.TempDir(), "roms")
	for _, path := range []string{"gba/a.gba", "gba/b.gba", "gba/.hidden", "gba/gamelist.xml", "wii/Some Game/game.wbfs", "n64/media/box.png"} {
		os.MkdirAll(filepath.Dir(filepath.Join(roms, path)), 0o755)
		os.WriteFile(filepath.Join(roms, path), nil, 0o644)
	}
	figures, err := gamingLibrary(roms)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"Game Boy": 0, "Game Boy Color": 0, "Game Boy Advance": 2, "Nintendo 64": 0, "GameCube": 0, "Wii": 1}
	if len(figures) != len(want) {
		t.Fatalf("figures = %v", figures)
	}
	for _, figure := range figures {
		if figure.Value != want[figure.Label] {
			t.Errorf("%s = %d, want %d", figure.Label, figure.Value, want[figure.Label])
		}
	}
	if _, err := gamingLibrary(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing roms folder must fail")
	}
}

func TestOnlyGamingIsPrivileged(t *testing.T) {
	gaming := ScopedAnswers{Storage: "local-lvm", LibraryBinding: "games", Cores: 4, MemoryMiB: 6144, DiskGB: 96, Privileged: true}
	if err := gaming.ValidateFor(GamingApp); err != nil {
		t.Fatal(err)
	}
	unconfirmed := gaming
	unconfirmed.Privileged = false
	if unconfirmed.ValidateFor(GamingApp) == nil {
		t.Error("Gaming without the confirmation was accepted")
	}
	music := ScopedAnswers{Storage: "local-lvm", LibraryBinding: "music", Cores: 2, MemoryMiB: 1024, DiskGB: 64, Privileged: true}
	if music.ValidateFor(MusicApp) == nil {
		t.Error("a privileged Music container was accepted")
	}
	small := gaming
	small.MemoryMiB = 2048
	if small.ValidateFor(GamingApp) == nil {
		t.Error("Gaming with 2 GiB was accepted")
	}
}

func TestGamingGamesListsTitlesAndOwnCovers(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"roms/gba/Metroid Fusion (USA).gba",
		"roms/gba/Mario Kart - Super Circuit (USA).gba",
		"roms/wii/Wii Sports (USA)/game.wbfs",
		"covers/gba/Metroid Fusion (USA).png",
		"covers/gba/notes.txt",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755)
		os.WriteFile(filepath.Join(root, path), []byte("x"), 0o644)
	}
	systems, err := gamingGames(filepath.Join(root, "roms"), filepath.Join(root, "covers"))
	if err != nil {
		t.Fatal(err)
	}
	gba, wii := systems[2], systems[5]
	if gba.ID != "gba" || len(gba.Games) != 2 || wii.ID != "wii" || len(wii.Games) != 1 {
		t.Fatalf("systems = %+v", systems)
	}
	for _, game := range gba.Games {
		if game.Name == "Metroid Fusion (USA)" && (!game.Cover || game.Title != "Metroid Fusion") {
			t.Errorf("metroid = %+v", game)
		}
		if game.Name == "Mario Kart - Super Circuit (USA)" && game.Cover {
			t.Errorf("mario kart has no cover of its own: %+v", game)
		}
	}
	if wii.Games[0].Name != "Wii Sports (USA)" {
		t.Errorf("a disc folder keeps its whole name: %+v", wii.Games[0])
	}

	data, contentType, err := gamingCover(filepath.Join(root, "covers"), "gba", "Metroid Fusion (USA)")
	if err != nil || contentType != "image/png" || string(data) != "x" {
		t.Fatalf("cover = %q, %q, %v", data, contentType, err)
	}
	for _, name := range []string{"Mario Kart - Super Circuit (USA)", "../../roms/gba/Metroid Fusion (USA)", "..", ".hidden", "a/b"} {
		if _, _, err := gamingCover(filepath.Join(root, "covers"), "gba", name); err == nil {
			t.Errorf("%q gave a cover", name)
		}
	}
	if _, _, err := gamingCover(filepath.Join(root, "covers"), "snes", "Metroid Fusion (USA)"); err == nil {
		t.Error("an unknown system gave a cover")
	}
}

func TestGameTitle(t *testing.T) {
	for name, want := range map[string]string{
		"Legend of Zelda, The - Ocarina of Time (USA) (Rev 2)": "Legend of Zelda, The - Ocarina of Time",
		"Tetris [!]":     "Tetris",
		"(Unl) Homebrew": "(Unl) Homebrew",
		"Plain":          "Plain",
	} {
		if got := GameTitle(name); got != want {
			t.Errorf("GameTitle(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestWolfSessionsDropTheStreamKeys(t *testing.T) {
	// The shape of GET /api/v1/sessions in the pinned Wolf image.
	data := `{"success":true,"sessions":[
		{"client_ip":"192.168.1.50","aes_key":"SECRETKEY","aes_iv":"SECRETIV","rtsp_fake_ip":"10.0.0.1","video_width":1920,"video_height":1080,"video_refresh_rate":60,"audio_channel_count":2,"app_id":"1","client_id":"4242"},
		{"client_ip":"192.168.1.60","aes_key":"x","aes_iv":"y","rtsp_fake_ip":"","video_width":1280,"video_height":720,"video_refresh_rate":60,"audio_channel_count":2,"client_id":"77"},
		{"client_ip":"192.168.1.70","aes_key":"x","aes_iv":"y","rtsp_fake_ip":"","video_width":1,"video_height":1,"video_refresh_rate":1,"audio_channel_count":2,"client_id":"../stop"},
		{"client_ip":"192.168.1.80","aes_key":"x","aes_iv":"y","rtsp_fake_ip":"","video_width":1,"video_height":1,"video_refresh_rate":1,"audio_channel_count":2}
	]}`
	sessions, err := wolfSessions([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0] != (GamingSession{ID: "4242", ClientIP: "192.168.1.50", Width: 1920, Height: 1080, FPS: 60}) || sessions[1].ID != "77" {
		t.Fatalf("sessions = %+v", sessions)
	}
	out, _ := json.Marshal(sessions)
	if strings.Contains(string(out), "SECRET") || strings.Contains(string(out), "aes") {
		t.Fatalf("stream keys leaked: %s", out)
	}
	if _, err := wolfSessions([]byte("not json")); err == nil {
		t.Fatal("accepted an invalid list")
	}
}

func TestStopGamingSessionChecksTheID(t *testing.T) {
	for _, id := range []string{"", "abc", "1;reboot", "../1", "123456789012345678901"} {
		if err := stopGamingSession(t.Context(), 120, id); err != ErrNoWolfSession {
			t.Errorf("%q: err = %v", id, err)
		}
	}
}

func TestGamingReadyNamesWhatIsMissing(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "etc/udev/rules.d"), 0o755)
	os.WriteFile(filepath.Join(root, "etc/udev/rules.d/85-wolf.rules"), nil, 0o644)
	// Regular files are not devices, so the three devices count as missing.
	os.MkdirAll(filepath.Join(root, "dev/dri"), 0o755)
	os.WriteFile(filepath.Join(root, "dev/uinput"), nil, 0o644)
	got := checkGamingReady(root)
	if got.Ready || len(got.Missing) != 3 || !strings.Contains(got.Missing[1], "uinput") {
		t.Fatalf("readiness = %+v", got)
	}
}
