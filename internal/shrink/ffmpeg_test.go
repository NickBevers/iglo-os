package shrink

import (
	"strings"
	"testing"
)

func TestArguments(t *testing.T) {
	for _, kind := range []string{"anime", "anime-movies", "movies", "series"} {
		for _, gpu := range []bool{false, true} {
			for _, profile := range []string{"standard", "high"} {
				p := sourceProbe()
				a := strings.Join(Arguments(p, kind, profile, "in.mkv", "out.mkv", gpu), " ")
				anime := kind == "anime" || kind == "anime-movies"
				if strings.Contains(a, "-map 0:2") == anime {
					t.Fatal(a)
				}
				if !strings.Contains(a, "-c:a:0 libopus -b:a:0 160k") {
					t.Fatal(a)
				}
				if strings.Contains(a, "-c:a:1 libopus") {
					t.Fatal("lossy audio not copied", a)
				}
				if gpu {
					q := "21"
					if profile == "high" {
						q = "17"
					}
					if !strings.Contains(a, "-c:v hevc_qsv -profile:v main10 -preset veryslow -global_quality:v "+q) {
						t.Fatal(a)
					}
				} else {
					q := "19"
					if profile == "high" {
						q = "17"
					}
					if !strings.Contains(a, "-c:v libx265 -preset slow -crf "+q) || !strings.Contains(a, "yuv420p10le") {
						t.Fatal(a)
					}
				}
				for _, keep := range []string{"-map 0:s?", "-map 0:t?", "-map_metadata 0", "-map_chapters 0", "-c:s copy", "-c:t copy"} {
					if !strings.Contains(a, keep) {
						t.Fatal(a)
					}
				}
			}
		}
	}
}
func TestAudioRatesAndFallback(t *testing.T) {
	for _, tc := range []struct {
		channels int
		rate     string
	}{{2, "160k"}, {6, "384k"}, {8, "512k"}} {
		p := sourceProbe()
		p.Streams[1].Channels = tc.channels
		a := strings.Join(Arguments(p, "anime", "standard", "in", "out", false), " ")
		if !strings.Contains(a, "-b:a:0 "+tc.rate) {
			t.Fatal(a)
		}
		if strings.Contains(a, "-mapping_family:a:0 1") != (tc.channels > 2) {
			t.Fatal(a)
		}
	}
	for _, codec := range []string{"flac", "truehd", "pcm_s24le", "alac", "dts"} {
		s := Stream{Codec: codec, Profile: "DTS-HD MA"}
		if !lossless(s) {
			t.Fatal(codec)
		}
	}
	if lossless(Stream{Codec: "dts", Profile: "DTS"}) {
		t.Fatal("lossy DTS changed")
	}
	p := sourceProbe()
	p.Streams[1].Tags["language"] = "eng"
	if len(keptAudio(p, "anime")) != 2 {
		t.Fatal("no Japanese fallback dropped tracks")
	}
}
func TestEstimatesAndVerification(t *testing.T) {
	p := sourceProbe()
	standard := Estimate(p, "anime", "standard")
	if standard != 307800000 {
		t.Fatal(standard)
	}
	p.Streams[0].Width = 1280
	p.Streams[0].Height = 720
	if Estimate(p, "anime", "standard") >= standard {
		t.Fatal("no resolution scaling")
	}
	if Estimate(p, "anime", "high") <= Estimate(p, "anime", "standard") {
		t.Fatal("no profile scaling")
	}
	src, out := sourceProbe(), resultProbe()
	if err := Verify(src, out, "anime"); err != nil {
		t.Fatal(err)
	}
	out.Format.Duration = "1442"
	if err := Verify(src, out, "anime"); err != nil {
		t.Fatal("0.2 percent tolerance", err)
	}
	out.Format.Duration = "1444"
	if err := Verify(src, out, "anime"); err == nil {
		t.Fatal("duration mismatch accepted")
	}
	if CheckSize(1000, 900) == nil || CheckSize(1000, 899) != nil {
		t.Fatal("90 percent gate")
	}
}
