package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	{"wii", "Wii"},
}

// gamingSharesRoot is where the host mounts the share bindings.
const gamingSharesRoot = "/mnt/iglo/shares/"

// notGames are files that frontends and scrapers leave next to the games.
var notGames = map[string]bool{"gamelist.xml": true, "media": true, "images": true, "videos": true, "manuals": true}

// GamingStats counts the games per system on the Games share of the gaming
// container. It reads the share on the host, so the container may be stopped.
func GamingStats(ctx context.Context, vmid int) (AppStats, error) {
	if vmid < 100 || vmid > 999999999 {
		return AppStats{}, ErrInvalidContainer
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	config, err := exec.CommandContext(ctx, "pct", "config", strconv.Itoa(vmid)).Output()
	if err != nil {
		return AppStats{}, errors.New("could not inspect the gaming container")
	}
	root, err := gamesShare(string(config))
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

// gamingLibrary counts the entries in each system folder. A game is a file
// or a folder, like an extracted disc. A missing system folder counts 0.
func gamingLibrary(roms string) ([]Figure, error) {
	if info, err := os.Stat(roms); err != nil || !info.IsDir() {
		return nil, errors.New("the Games share has no roms folder, or it is unavailable")
	}
	figures := make([]Figure, 0, len(GamingSystems))
	for _, system := range GamingSystems {
		entries, err := os.ReadDir(filepath.Join(roms, system.Folder))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("could not read the " + system.Label + " folder on the Games share")
		}
		count := int64(0)
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || notGames[strings.ToLower(name)] {
				continue
			}
			count++
		}
		figures = append(figures, Figure{Label: system.Label, Value: count})
	}
	return figures, nil
}
