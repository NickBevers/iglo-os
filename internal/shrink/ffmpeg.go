package shrink

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Stream struct {
	Duration    string            `json:"duration"`
	Index       int               `json:"index"`
	Type        string            `json:"codec_type"`
	Codec       string            `json:"codec_name"`
	Profile     string            `json:"profile"`
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	Pixel       string            `json:"pix_fmt"`
	Bits        string            `json:"bits_per_raw_sample"`
	Transfer    string            `json:"color_transfer"`
	Channels    int               `json:"channels"`
	BitRate     string            `json:"bit_rate"`
	AvgRate     string            `json:"avg_frame_rate"`
	BaseRate    string            `json:"r_frame_rate"`
	Tags        map[string]string `json:"tags"`
	Disposition map[string]int    `json:"disposition"`
	SideData    []struct {
		Type string `json:"side_data_type"`
	} `json:"side_data_list"`
}
type Probe struct {
	Streams  []Stream          `json:"streams"`
	Chapters []json.RawMessage `json:"chapters"`
	Format   struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (p Probe) Duration() float64 {
	if d := p.VideoDuration(); d > 0 {
		return d
	}
	d, _ := strconv.ParseFloat(p.Format.Duration, 64)
	return d
}
func (p Probe) VideoDuration() float64 {
	v, err := p.Video()
	if err != nil {
		return 0
	}
	if d, err := strconv.ParseFloat(v.Duration, 64); err == nil && validDuration(d) {
		return d
	}
	for key, value := range v.Tags {
		if strings.EqualFold(key, "DURATION") || strings.EqualFold(key, "DURATION-eng") {
			parts := strings.Split(value, ":")
			if len(parts) != 3 {
				continue
			}
			hours, e1 := strconv.ParseFloat(parts[0], 64)
			minutes, e2 := strconv.ParseFloat(parts[1], 64)
			seconds, e3 := strconv.ParseFloat(parts[2], 64)
			if e1 == nil && e2 == nil && e3 == nil {
				d := hours*3600 + minutes*60 + seconds
				if validDuration(d) {
					return d
				}
			}
		}
	}
	return 0
}

// FrameRate is the primary video's average frame rate, or its base rate when
// FFprobe has no average. It is 0 when neither is known.
func (p Probe) FrameRate() float64 {
	v, err := p.Video()
	if err != nil {
		return 0
	}
	for _, rate := range []string{v.AvgRate, v.BaseRate} {
		num, den, ok := strings.Cut(rate, "/")
		if !ok {
			continue
		}
		n, e1 := strconv.ParseFloat(num, 64)
		d, e2 := strconv.ParseFloat(den, 64)
		if e1 == nil && e2 == nil && n > 0 && d > 0 {
			return n / d
		}
	}
	return 0
}

// readProgress turns FFmpeg's -progress blocks into a fraction of the source.
// FFmpeg 7.1 reports out_time_us as the lowest DTS over all output streams and
// prints N/A while any stream has written nothing (fftools/ffmpeg_sched.c,
// trailing_dts, Jellyfin FFmpeg v7.1.4-3, checked 2026-11-11). Matroska
// attachments never write a packet, so with fonts mapped out_time_us stays N/A
// for the whole encode. The frame line counts packets of the first video
// output, which is the primary video here, so it is the fallback.
func readProgress(r io.Reader, p Probe, progress func(float64)) error {
	duration := p.Duration()
	frames := duration * p.FrameRate()
	report := func(v float64) {
		if !math.IsNaN(v) && !math.IsInf(v, 0) {
			progress(math.Max(0, math.Min(0.99, v)))
		}
	}
	var frame float64
	timed := false
	scan := bufio.NewScanner(r)
	for scan.Scan() {
		key, value, ok := strings.Cut(scan.Text(), "=")
		if !ok {
			continue
		}
		switch key {
		case "frame":
			frame, _ = strconv.ParseFloat(value, 64)
		case "out_time_us":
			v, err := strconv.ParseFloat(value, 64)
			timed = err == nil
			if timed && duration > 0 {
				report(v / 1e6 / duration)
			}
		case "progress":
			// One block ends here. Use frames only when it had no time.
			if !timed && frames > 0 {
				report(frame / frames)
			}
			timed = false
		}
	}
	return scan.Err()
}

func validDuration(d float64) bool { return d > 0 && !math.IsNaN(d) && !math.IsInf(d, 0) }
func (p Probe) UnsupportedReason() string {
	videos := 0
	for _, s := range p.Streams {
		switch s.Type {
		case "video":
			if s.Disposition["attached_pic"] == 0 {
				videos++
			}
		case "data":
			return "data streams are not supported"
		case "audio", "subtitle", "attachment":
		default:
			return "unsupported stream type"
		}
	}
	if videos != 1 {
		return "use exactly one primary video stream"
	}
	if !validDuration(p.VideoDuration()) {
		return "video stream duration is unavailable"
	}
	return ""
}
func coverStreams(p Probe) []Stream {
	covers := []Stream{}
	for _, s := range p.Streams {
		if s.Type == "video" && s.Disposition["attached_pic"] != 0 {
			covers = append(covers, s)
		}
	}
	return covers
}
func (p Probe) Video() (Stream, error) {
	for _, s := range p.Streams {
		if s.Type == "video" && s.Disposition["attached_pic"] == 0 {
			return s, nil
		}
	}
	return Stream{}, errors.New("no video stream")
}
func (s Stream) HDR() bool {
	if s.Transfer == "smpte2084" || s.Transfer == "arib-std-b67" {
		return true
	}
	for _, d := range s.SideData {
		v := strings.ToLower(d.Type)
		if strings.Contains(v, "dovi") || strings.Contains(v, "dolby vision") {
			return true
		}
	}
	return false
}
func (s Stream) Depth() int {
	if b, _ := strconv.Atoi(s.Bits); b > 0 {
		return b
	}
	for _, b := range []int{16, 14, 12, 10, 9} {
		if strings.Contains(s.Pixel, strconv.Itoa(b)) {
			return b
		}
	}
	return 8
}
func keptAudio(p Probe, kind string) []Stream {
	all, jpn := []Stream{}, []Stream{}
	for _, s := range p.Streams {
		if s.Type == "audio" {
			all = append(all, s)
			if s.Tags["language"] == "jpn" {
				jpn = append(jpn, s)
			}
		}
	}
	if (kind == "anime" || kind == "anime-movies") && len(jpn) > 0 {
		return jpn
	}
	return all
}
func lossless(s Stream) bool {
	return s.Codec == "flac" || s.Codec == "truehd" || s.Codec == "alac" || strings.HasPrefix(s.Codec, "pcm_") || (s.Codec == "dts" && (strings.Contains(strings.ToUpper(s.Profile), "MA") || strings.Contains(strings.ToUpper(s.Profile), "MASTER")))
}
func opusRate(ch int) int64 {
	if ch > 6 {
		return 512000
	}
	if ch > 2 {
		return 384000
	}
	return 160000
}
func Estimate(p Probe, kind, profile string) int64 {
	v, err := p.Video()
	if err != nil {
		return 0
	}
	rate := 1550000.0
	if profile == "high" {
		rate = 2100000
	}
	rate *= float64(v.Width) * float64(v.Height) / (1920 * 1080)
	for _, a := range keptAudio(p, kind) {
		if lossless(a) {
			rate += float64(opusRate(a.Channels))
		} else {
			b, _ := strconv.ParseFloat(a.BitRate, 64)
			if b <= 0 {
				for _, key := range []string{"BPS", "BPS-eng"} {
					if value, e := strconv.ParseFloat(a.Tags[key], 64); e == nil && value > 0 {
						b = value
						break
					}
				}
			}
			if b <= 0 {
				b = float64(opusRate(a.Channels))
			}
			rate += b
		}
	}
	return int64(p.Duration() * rate / 8)
}

// Jellyfin FFmpeg 7 QSV settings were measured on the owner's UHD 770,
// checked 2026-11-08. Debian's QSV runtime is not a substitute. Metadata and
// dispositions are mapped explicitly so removing an audio stream keeps them.
func Arguments(p Probe, kind, profile, input, output string, gpu bool) []string {
	a := []string{"-nostdin", "-v", "error", "-n", "-progress", "pipe:1"}
	if gpu {
		a = append(a, "-init_hw_device", "vaapi=va:/dev/dri/renderD128", "-init_hw_device", "qsv=qs@va", "-filter_hw_device", "qs", "-hwaccel", "qsv", "-hwaccel_output_format", "qsv")
	}
	video, _ := p.Video()
	a = append(a, "-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm", "-i", input, "-map", fmt.Sprintf("0:%d", video.Index))
	covers := coverStreams(p)
	for _, cover := range covers {
		a = append(a, "-map", fmt.Sprintf("0:%d", cover.Index))
	}
	audio := keptAudio(p, kind)
	for _, s := range audio {
		a = append(a, "-map", fmt.Sprintf("0:%d", s.Index))
	}
	a = append(a, "-map", "0:s?", "-map", "0:t?", "-map_metadata", "0", "-map_chapters", "0", "-c:v", "copy", "-c:a", "copy", "-c:s", "copy", "-c:t", "copy")
	if gpu {
		q := "21"
		if profile == "high" {
			q = "17"
		}
		a = append(a, "-c:v:0", "hevc_qsv", "-profile:v:0", "main10", "-preset:v:0", "veryslow", "-global_quality:v:0", q)
	} else {
		q := "19"
		if profile == "high" {
			q = "17"
		}
		a = append(a, "-c:v:0", "libx265", "-preset:v:0", "slow", "-crf:v:0", q, "-pix_fmt:v:0", "yuv420p10le", "-x265-params:v:0", "aq-mode=3:no-sao=1:pools=2:frame-threads=2")
	}
	groups := map[string]int{}
	selected := []Stream{}
	v, _ := p.Video()
	selected = append(selected, v)
	selected = append(selected, covers...)
	selected = append(selected, audio...)
	for _, typ := range []string{"subtitle", "attachment"} {
		for _, s := range p.Streams {
			if s.Type == typ {
				selected = append(selected, s)
			}
		}
	}
	specs := map[string]string{"video": "v", "audio": "a", "subtitle": "s", "attachment": "t"}
	for _, s := range selected {
		spec := fmt.Sprintf("%s:%d", specs[s.Type], groups[s.Type])
		groups[s.Type]++
		names := []string{}
		for name, on := range s.Disposition {
			if on != 0 {
				names = append(names, name)
			}
		}
		// Sort map keys for stable command arguments and tests.
		slices.Sort(names)
		disposition := "0"
		if len(names) > 0 {
			disposition = strings.Join(names, "+")
		}
		a = append(a, "-disposition:"+spec, disposition)
		if (s.Type == "video" && s.Disposition["attached_pic"] == 0) || (s.Type == "audio" && lossless(s)) {
			for _, tag := range statisticsTags(s.Tags) {
				a = append(a, "-metadata:s:"+spec, tag+"=")
			}
		}
		if s.Type == "audio" && lossless(s) {
			a = append(a, "-c:"+spec, "libopus", "-b:"+spec, strconv.FormatInt(opusRate(s.Channels)/1000, 10)+"k")
			if s.Channels > 2 {
				a = append(a, "-mapping_family:"+spec, "1")
			}
		}
	}
	return append(a, output)
}
func statisticsTags(tags map[string]string) []string {
	out := []string{}
	for tag := range tags {
		upper := strings.ToUpper(tag)
		if strings.HasPrefix(upper, "_STATISTICS_") {
			out = append(out, tag)
			continue
		}
		for _, base := range []string{"BPS", "NUMBER_OF_BYTES", "NUMBER_OF_FRAMES", "DURATION"} {
			if upper == base || strings.HasPrefix(upper, base+"-") {
				out = append(out, tag)
				break
			}
		}
	}
	slices.Sort(out)
	return out
}
func Verify(src, out Probe, kind string) error {
	if reason := src.UnsupportedReason(); reason != "" {
		return errors.New(reason)
	}
	if reason := out.UnsupportedReason(); reason != "" {
		return errors.New(reason)
	}
	d, v := src.VideoDuration(), out.VideoDuration()
	if !validDuration(d) || !validDuration(v) || math.Round(math.Abs(v-d)*1e9) > math.Round(math.Min(3, 1+d*0.001)*1e9) {
		return errors.New("video duration verification failed")
	}
	if len(src.Chapters) != len(out.Chapters) {
		return errors.New("chapter count verification failed")
	}
	count := func(p Probe, typ string) int {
		n := 0
		for _, s := range p.Streams {
			if s.Type == typ {
				n++
			}
		}
		return n
	}
	if count(out, "video") != 1+len(coverStreams(src)) {
		return errors.New("video stream verification failed")
	}
	covers, originalCovers := coverStreams(out), coverStreams(src)
	if len(covers) != len(originalCovers) {
		return errors.New("cover image verification failed")
	}
	for i, cover := range covers {
		before := originalCovers[i]
		if cover.Codec != before.Codec || cover.Width != before.Width || cover.Height != before.Height {
			return errors.New("cover image verification failed")
		}
	}
	video, _ := out.Video()
	sourceVideo, _ := src.Video()
	if video.Codec != "hevc" || video.Depth() != 10 || video.Width != sourceVideo.Width || video.Height != sourceVideo.Height {
		return errors.New("HEVC Main10 verification failed")
	}
	for _, typ := range []string{"subtitle", "attachment"} {
		if count(src, typ) != count(out, typ) {
			return fmt.Errorf("%s stream verification failed", typ)
		}
	}
	expected := keptAudio(src, kind)
	actual := []Stream{}
	for _, s := range out.Streams {
		if s.Type == "audio" {
			actual = append(actual, s)
		}
	}
	if len(expected) != len(actual) {
		return errors.New("audio stream count verification failed")
	}
	for i, s := range expected {
		codec := s.Codec
		if lossless(s) {
			codec = "opus"
		}
		if actual[i].Codec != codec || actual[i].Channels != s.Channels || actual[i].Tags["language"] != s.Tags["language"] {
			return errors.New("audio stream verification failed")
		}
	}
	return nil
}

var ErrNotSmaller = errors.New("result is not below 90 percent of source size")

func CheckSize(source, result int64) error {
	if result <= 0 || float64(result) >= float64(source)*0.9 {
		return ErrNotSmaller
	}
	return nil
}

type Encoder struct {
	FFmpeg, FFprobe string
	GPU             bool
}

func NewEncoder() *Encoder {
	return &Encoder{FFmpeg: "/usr/lib/jellyfin-ffmpeg/ffmpeg", FFprobe: "/usr/lib/jellyfin-ffmpeg/ffprobe"}
}

// Process groups let cancellation stop nice/ionice and all their children.
func lowCommand(ctx context.Context, binary string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "nice", append([]string{"-n", "15", "ionice", "-c", "3", binary}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("tool output too large")
	}
	return b.Buffer.Write(p)
}
func (e *Encoder) Probe(ctx context.Context, f *os.File) (Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	f.Seek(0, 0)
	cmd := lowCommand(ctx, e.FFprobe, "-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,avi,mpegts,mpeg", "-show_streams", "-show_format", "-show_chapters", "-of", "json", "/dev/fd/3")
	cmd.ExtraFiles = []*os.File{f}
	b := &limitedBuffer{limit: 1 << 20}
	cmd.Stdout = b
	cmd.Stderr = &limitedBuffer{limit: 8192}
	if err := cmd.Run(); err != nil {
		return Probe{}, fmt.Errorf("ffprobe failed: %w", err)
	}
	var p Probe
	if err := json.Unmarshal(b.Bytes(), &p); err != nil {
		return p, err
	}
	d := p.Duration()
	v, err := p.Video()
	if err != nil || d <= 0 || math.IsNaN(d) || math.IsInf(d, 0) || v.Width <= 0 || v.Height <= 0 {
		return p, errors.New("invalid video probe")
	}
	return p, nil
}
func (e *Encoder) DetectGPU(ctx context.Context) bool {
	e.GPU = false
	if _, err := os.Stat("/dev/dri/renderD128"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := lowCommand(ctx, e.FFmpeg, "-nostdin", "-v", "error", "-init_hw_device", "vaapi=va:/dev/dri/renderD128", "-init_hw_device", "qsv=qs@va", "-filter_hw_device", "qs", "-f", "lavfi", "-i", "color=s=128x128:d=0.2", "-vf", "format=p010le,hwupload=extra_hw_frames=16", "-c:v", "hevc_qsv", "-profile:v", "main10", "-preset", "veryslow", "-global_quality:v", "21", "-f", "null", "-")
	cmd.Stdout = &limitedBuffer{limit: 8192}
	cmd.Stderr = &limitedBuffer{limit: 8192}
	e.GPU = cmd.Run() == nil
	return e.GPU
}
func (e *Encoder) Encode(ctx context.Context, f *os.File, out string, p Probe, kind, profile string, progress func(float64)) error {
	if reason := p.UnsupportedReason(); reason != "" {
		return errors.New(reason)
	}
	cmd := lowCommand(ctx, e.FFmpeg, Arguments(p, kind, profile, "/dev/fd/3", out, e.GPU)...)
	cmd.ExtraFiles = []*os.File{f}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = &limitedBuffer{limit: 32768}
	if err = cmd.Start(); err != nil {
		return err
	}
	scanErr := readProgress(pipe, p, progress)
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w", err)
	}
	return scanErr
}
