package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEveryImageIsPinned keeps the stacks on images that a test saw: a tag
// alone can move to a new image without a release of Iglo-OS.
func TestEveryImageIsPinned(t *testing.T) {
	files, _ := filepath.Glob("../../deploy/stacks/*/*")
	reference := regexp.MustCompile(`(?m)^\s*image\s*[:=]\s*"?([^"\s#]+)`)
	checked := 0
	for _, file := range files {
		// Minecraft is dropped and stays as it was.
		if strings.Contains(file, "/minecraft/") || !(strings.HasSuffix(file, ".yaml") || strings.HasSuffix(file, ".toml")) {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range reference.FindAllStringSubmatch(string(data), -1) {
			image := match[1]
			checked++
			// Immich is pinned by its release, which the installer writes in .env.
			if strings.Contains(image, "${IMMICH_VERSION") {
				continue
			}
			if !regexp.MustCompile(`@sha256:[a-f0-9]{64}$`).MatchString(image) {
				t.Errorf("%s: %s has no digest; run go run ./dev/pinimages %s", file, image, file)
			}
		}
	}
	if checked < 10 {
		t.Fatalf("found only %d images; did the stacks move?", checked)
	}
}

func TestImageLine(t *testing.T) {
	for line, repo := range map[string]string{
		"    image: lscr.io/linuxserver/radarr:latest":                                       "lscr.io/linuxserver/radarr",
		"    image: qmcgaw/gluetun:v3.41.3@sha256:" + strings.Repeat("a", 64) + " # v3.41.3": "qmcgaw/gluetun",
	} {
		if match := imageLine.FindStringSubmatch(line); match == nil || match[2] != repo {
			t.Errorf("%q: %v", line, match)
		}
	}
	if imageLine.MatchString("    image: ghcr.io/immich-app/immich-server:${IMMICH_VERSION:-release}") {
		t.Error("a templated image matched")
	}
}
