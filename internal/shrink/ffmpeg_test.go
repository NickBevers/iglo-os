package shrink

import (
	"math"
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
					if !strings.Contains(a, "-c:v:0 hevc_qsv -profile:v:0 main10 -preset:v:0 veryslow -global_quality:v:0 "+q) {
						t.Fatal(a)
					}
				} else {
					q := "19"
					if profile == "high" {
						q = "17"
					}
					if !strings.Contains(a, "-c:v:0 libx265 -preset:v:0 slow -crf:v:0 "+q) || !strings.Contains(a, "yuv420p10le") {
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
	out.Streams[0].Duration = "1442"
	if err := Verify(src, out, "anime"); err != nil {
		t.Fatal("video duration tolerance", err)
	}
	out.Streams[0].Duration = "1444"
	if err := Verify(src, out, "anime"); err == nil {
		t.Fatal("duration mismatch accepted")
	}
	if CheckSize(1000, 900) == nil || CheckSize(1000, 899) != nil {
		t.Fatal("90 percent gate")
	}
}

// progressBlock is one -progress block as Jellyfin FFmpeg 7.1.4 prints it.
func progressBlock(frame, outTime string) string {
	return "frame=" + frame + "\nfps=23.98\nstream_0_0_q=-0.0\nbitrate=N/A\ntotal_size=7340032\nout_time_us=" + outTime +
		"\nout_time_ms=" + outTime + "\nout_time=N/A\ndup_frames=0\ndrop_frames=0\nspeed=N/A\nprogress=continue\n"
}

func TestReadProgress(t *testing.T) {
	p := sourceProbe()
	p.Streams[0].AvgRate = "24000/1001"
	read := func(p Probe, stream string) []float64 {
		t.Helper()
		got := []float64{}
		if err := readProgress(strings.NewReader(stream), p, func(v float64) { got = append(got, v) }); err != nil {
			t.Fatal(err)
		}
		return got
	}
	near := func(got []float64, want ...float64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range want {
			if math.Abs(got[i]-want[i]) > 0.001 {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	}
	// With fonts mapped, out_time_us stays N/A, so frames give the fraction.
	// 1440 seconds at 23.976 fps is 34525 frames.
	near(read(p, progressBlock("8631", "N/A")+progressBlock("17263", "N/A")), 0.25, 0.5)
	// A real output time wins over frames.
	near(read(p, progressBlock("1", "720000000")), 0.5)
	// The base rate is the fallback, and the end stays below done.
	p.Streams[0].AvgRate, p.Streams[0].BaseRate = "0/0", "24/1"
	near(read(p, progressBlock("999999", "N/A")), 0.99)
	// Without a frame rate there is nothing to report.
	p.Streams[0].BaseRate = ""
	near(read(p, progressBlock("100", "N/A")))
}
