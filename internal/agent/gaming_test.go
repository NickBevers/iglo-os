package agent

import (
	"os"
	"path/filepath"
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
