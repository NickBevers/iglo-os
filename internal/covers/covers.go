// Package covers fetches game box art from the libretro thumbnail server and
// keeps a copy, so each cover is downloaded once. It needs no account. The
// server sees the game names that Iglo-OS looks up.
package covers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotFound means libretro has no box art for the game.
var ErrNotFound = errors.New("no cover found")

// Systems are the libretro folders of the system folders on the Games share.
var Systems = map[string]string{
	"gb":  "Nintendo - Game Boy",
	"gbc": "Nintendo - Game Boy Color",
	"gba": "Nintendo - Game Boy Advance",
	"n64": "Nintendo - Nintendo 64",
	"gc":  "Nintendo - GameCube",
}

const (
	// retryMiss is how long a game without box art waits before the next try.
	retryMiss = 7 * 24 * time.Hour
	maxCover  = 5 << 20
	// coverWidth is the widest a kept copy gets. The grid shows covers at about
	// 200 pixels, so this stays sharp on a high-density screen.
	coverWidth = 400
)

// ContentType is the type of every cover that the Store returns.
const ContentType = "image/jpeg"

type Store struct {
	// Dir holds the downloaded covers and the misses.
	Dir string
	// Base is the thumbnail server. Tests replace it.
	Base string
	HTTP *http.Client
	Now  func() time.Time
	// slots limits the downloads at the same time, for a page full of covers.
	slots chan struct{}
}

func New(dir string) *Store {
	return &Store{Dir: dir, Base: "https://thumbnails.libretro.com", HTTP: &http.Client{Timeout: 15 * time.Second}, Now: time.Now, slots: make(chan struct{}, 4)}
}

// Cover returns the box art of a game as a small JPEG, from the copy or from libretro.
func (s *Store) Cover(ctx context.Context, system, name string) ([]byte, error) {
	folder, ok := Systems[system]
	if !ok || name == "" {
		return nil, ErrNotFound
	}
	sum := sha256.Sum256([]byte(name))
	file := filepath.Join(s.Dir, system, hex.EncodeToString(sum[:]))
	if data, err := os.ReadFile(file + ".jpg"); err == nil {
		return data, nil
	}
	if info, err := os.Stat(file + ".miss"); err == nil && s.Now().Sub(info.ModTime()) < retryMiss {
		return nil, ErrNotFound
	}

	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Like RetroArch: the full name first, then the name without its tags.
	var data []byte
	var err error
	for _, candidate := range candidates(name) {
		data, err = s.download(ctx, folder, candidate)
		if !errors.Is(err, ErrNotFound) {
			break
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return data, nil
	}
	switch {
	case err == nil:
		os.WriteFile(file+".jpg", data, 0o600)
		os.Remove(file + ".miss")
	case errors.Is(err, ErrNotFound):
		os.WriteFile(file+".miss", nil, 0o600)
		os.Chtimes(file+".miss", s.Now(), s.Now())
	}
	return data, err
}

// candidates are the thumbnail names to try for a game name.
func candidates(name string) []string {
	names := []string{thumbnailName(name)}
	if index := strings.Index(name, "("); index > 0 {
		if short := thumbnailName(strings.TrimSpace(name[:index])); short != names[0] {
			names = append(names, short)
		}
	}
	return names
}

// thumbnailName replaces the characters that libretro thumbnails can't have.
func thumbnailName(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("&*/:`<>?\\|\"", r) {
			return '_'
		}
		return r
	}, name)
}

func (s *Store) download(ctx context.Context, folder, name string) ([]byte, error) {
	address := s.Base + "/" + url.PathEscape(folder) + "/Named_Boxarts/" + url.PathEscape(name+".png")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := s.HTTP.Do(request)
	if err != nil {
		return nil, fmt.Errorf("the thumbnail server did not answer: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the thumbnail server answered %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxCover+1))
	if err != nil {
		return nil, err
	}
	// Only a PNG of a sane size. Anything else is not box art.
	if len(data) > maxCover || !strings.HasPrefix(string(data), "\x89PNG\r\n\x1a\n") {
		return nil, ErrNotFound
	}
	return shrink(data)
}

// shrink turns box art into a JPEG of at most coverWidth pixels wide, by
// averaging blocks of pixels. libretro PNGs are often half a megabyte.
func shrink(data []byte) ([]byte, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > 4096*4096 {
		return nil, ErrNotFound
	}
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrNotFound
	}
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > coverWidth {
		height = max(1, height*coverWidth/bounds.Dx())
		width = coverWidth
	}
	small := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		top, bottom := bounds.Min.Y+y*bounds.Dy()/height, bounds.Min.Y+max((y+1)*bounds.Dy()/height, y*bounds.Dy()/height+1)
		for x := range width {
			left, right := bounds.Min.X+x*bounds.Dx()/width, bounds.Min.X+max((x+1)*bounds.Dx()/width, x*bounds.Dx()/width+1)
			var red, green, blue, alpha, count uint64
			for sy := top; sy < bottom; sy++ {
				for sx := left; sx < right; sx++ {
					r, g, b, a := source.At(sx, sy).RGBA()
					red, green, blue, alpha, count = red+uint64(r), green+uint64(g), blue+uint64(b), alpha+uint64(a), count+1
				}
			}
			// JPEG has no transparency, so see-through parts become white.
			white := (count*0xffff - alpha) / count
			offset := small.PixOffset(x, y)
			small.Pix[offset] = uint8((red/count + white) >> 8)
			small.Pix[offset+1] = uint8((green/count + white) >> 8)
			small.Pix[offset+2] = uint8((blue/count + white) >> 8)
			small.Pix[offset+3] = 0xff
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, small, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
