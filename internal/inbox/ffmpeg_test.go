package inbox

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFFmpegProfiles(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg integration needs the local binary")
	}
	fp, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "source.mkv")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ff, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=128x96:rate=10:duration=1", "-f", "lavfi", "-i", "sine=frequency=1000:duration=1", "-c:v", "mpeg4", "-c:a", "pcm_s16le", "-threads", "2", "-shortest", input)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, data)
	}
	original, _ := os.ReadFile(input)
	hash := sha256.Sum256(original)
	e := Encoder{FFmpeg: ff, FFprobe: fp}
	for _, profile := range []string{"preserve", "compatible"} {
		t.Run(profile, func(t *testing.T) {
			stage := t.TempDir()
			output, err := e.Convert(ctx, input, stage, profile)
			if err != nil {
				t.Fatal(err)
			}
			p, err := e.probe(ctx, output)
			if err != nil {
				t.Fatal(err)
			}
			v, _ := p.video()
			if profile == "preserve" && v.Codec != "mpeg4" {
				t.Fatal(p)
			}
			if profile == "compatible" && v.Codec != "h264" {
				t.Fatal(p)
			}
		})
	}
	data, _ := os.ReadFile(input)
	if sha256.Sum256(data) != hash {
		t.Fatal("source changed")
	}
	// A failed VAAPI invocation must retry on CPU when a device disappears.
	t.Run("gpu failure falls back", func(t *testing.T) {
		e.GPU = true
		if _, err := e.Convert(ctx, input, t.TempDir(), "compatible"); err != nil {
			t.Fatal(err)
		}
	})
	bad := filepath.Join(dir, "invalid.mp4")
	os.WriteFile(bad, []byte("not a video"), 0600)
	if _, err := e.Convert(ctx, bad, t.TempDir(), "preserve"); err == nil {
		t.Fatal("published malformed input")
	}
}
func TestProbeValidation(t *testing.T) {
	p := Probe{Streams: []Stream{{Type: "video", Width: 1920, Height: 1080, Codec: "hevc", Pixel: "yuv420p10le", Transfer: "smpte2084"}}}
	p.Format.Duration = "60"
	after := p
	after.Streams = append([]Stream(nil), p.Streams...)
	after.Streams[0].Width = 1280
	if validateOutput(p, after, "preserve") == nil {
		t.Fatal("accepted changed dimensions")
	}
	after = p
	after.Format.Duration = "30"
	if validateOutput(p, after, "preserve") == nil {
		t.Fatal("accepted truncated file")
	}
	for _, duration := range []string{"NaN", "+Inf", "-1", "14401"} {
		p.Format.Duration = duration
		if _, err := p.duration(); err == nil {
			t.Fatal(duration)
		}
	}
}
func TestFFmpegSubtitles(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	fp, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe unavailable")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "caption.srt")
	os.WriteFile(sub, []byte("1\n00:00:00,000 --> 00:00:00,800\nCaption\n"), 0600)
	input := filepath.Join(dir, "subtitled.mkv")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if data, err := exec.CommandContext(ctx, ff, "-v", "error", "-f", "lavfi", "-i", "color=size=128x96:duration=1", "-i", sub, "-c:v", "libx264", "-threads", "2", "-c:s", "srt", input).CombinedOutput(); err != nil {
		t.Fatalf("fixture %v %s", err, data)
	}
	e := Encoder{FFmpeg: ff, FFprobe: fp}
	if _, err := e.Convert(ctx, input, t.TempDir(), "preserve"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Convert(ctx, input, t.TempDir(), "compatible"); err == nil || !strings.Contains(err.Error(), "subtitles") {
		t.Fatal(err)
	}
}
