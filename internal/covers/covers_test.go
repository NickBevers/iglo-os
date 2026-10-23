package covers

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// boxArt is a PNG of width by height, half see-through like some box art.
func boxArt(t *testing.T, width, height int) []byte {
	t.Helper()
	art := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			art.Set(x, y, color.NRGBA{R: 200, A: uint8(255 * (x % 2))})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, art); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestCoverTriesTheShortNameAndKeepsACopy(t *testing.T) {
	art := boxArt(t, 1200, 800)
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		if r.URL.Path == "/Nintendo - Game Boy Advance/Named_Boxarts/Metroid Fusion.png" {
			w.Write(art)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	store := New(t.TempDir())
	store.Base = server.URL

	data, err := store.Cover(context.Background(), "gba", "Metroid Fusion (USA) (Beta)")
	if err != nil {
		t.Fatal(err)
	}
	small, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil || small.Bounds().Dx() != coverWidth || small.Bounds().Dy() != 266 || len(data) >= len(art) {
		t.Fatalf("not a smaller JPEG: %v, %v, %d of %d bytes", small.Bounds(), err, len(data), len(art))
	}
	// Half the pixels were see-through, so the average turns pink, not dark red.
	if r, g, _, _ := small.At(200, 133).RGBA(); r>>8 < 200 || g>>8 < 90 {
		t.Errorf("transparency did not become white: r=%d g=%d", r>>8, g>>8)
	}
	if len(requests) != 2 || requests[0] != "/Nintendo - Game Boy Advance/Named_Boxarts/Metroid Fusion (USA) (Beta).png" {
		t.Fatalf("requests = %v", requests)
	}
	if again, err := store.Cover(context.Background(), "gba", "Metroid Fusion (USA) (Beta)"); err != nil || len(requests) != 2 || !bytes.Equal(again, data) {
		t.Fatalf("the copy was not used: %v, %v", err, requests)
	}
}

func TestCoverRemembersMissesForAWeek(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	}))
	defer server.Close()
	now := time.Now()
	store := New(t.TempDir())
	store.Base, store.Now = server.URL, func() time.Time { return now }

	for range 2 {
		if _, err := store.Cover(context.Background(), "n64", "Homebrew"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	}
	if requests != 1 {
		t.Fatalf("asked %d times within a week", requests)
	}
	now = now.Add(8 * 24 * time.Hour)
	store.Cover(context.Background(), "n64", "Homebrew")
	if requests != 2 {
		t.Fatalf("did not ask again after a week: %d", requests)
	}
}

func TestCoverRefusesWhatIsNotBoxArt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>not an image</html>"))
	}))
	defer server.Close()
	store := New(t.TempDir())
	store.Base = server.URL
	if _, err := store.Cover(context.Background(), "gb", "Tetris (World) (Rev 1)"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := store.Cover(context.Background(), "snes", "Tetris"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown system: %v", err)
	}
}

func TestThumbnailName(t *testing.T) {
	if got := thumbnailName(`Q*Bert's Qubes: "Deluxe" (USA)`); got != `Q_Bert's Qubes_ _Deluxe_ (USA)` {
		t.Fatalf("got %q", got)
	}
}
