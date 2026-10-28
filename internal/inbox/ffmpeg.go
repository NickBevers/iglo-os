package inbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const maxMediaBytes int64 = 16 << 30

type Stream struct {
	SideData []struct {
		Rotation int `json:"rotation"`
	} `json:"side_data_list"`
	Index       int    `json:"index"`
	Codec       string `json:"codec_name"`
	Type        string `json:"codec_type"`
	Pixel       string `json:"pix_fmt"`
	Transfer    string `json:"color_transfer"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Disposition struct {
		Attached int `json:"attached_pic"`
	} `json:"disposition"`
}
type Probe struct {
	Streams []Stream `json:"streams"`
	Format  struct {
		Name     string `json:"format_name"`
		Duration string `json:"duration"`
	} `json:"format"`
}

func (p Probe) duration() (float64, error) {
	n, err := strconv.ParseFloat(p.Format.Duration, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 4*60*60 {
		return 0, errors.New("video needs a valid duration of at most four hours")
	}
	return n, nil
}
func (p Probe) video() (Stream, error) {
	var video Stream
	count := 0
	for _, s := range p.Streams {
		if s.Type == "video" && s.Disposition.Attached == 0 {
			video = s
			count++
		}
	}
	if count != 1 || video.Width <= 0 || video.Height <= 0 || video.Width > 7680 || video.Height > 4320 {
		return video, errors.New("use one video stream, up to 8K")
	}
	return video, nil
}

// FFmpeg options and stream-copy behavior were checked against 7.1.5
// doc/ffmpeg.texi, protocols.texi and encoders.texi on 28 October 2026.
// Only fixed local staging paths reach FFmpeg; playlists and network
// protocols are refused, and source files are never an output target.
type Encoder struct {
	FFmpeg, FFprobe string
	GPU             bool
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > b.limit {
		return 0, errors.New("media diagnostic output exceeded its limit")
	}
	_, err := b.Buffer.Write(p)
	return n, err
}
func command(ctx context.Context, binary string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binary, args...)
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
func (e Encoder) probe(ctx context.Context, path string) (Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := command(ctx, e.FFprobe, "-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,avi,mpegts,mpeg", "-show_streams", "-show_format", "-of", "json", path)
	output := &boundedBuffer{limit: 256 << 10}
	diagnostic := &boundedBuffer{limit: 8192}
	cmd.Stdout, cmd.Stderr = output, diagnostic
	if err := cmd.Run(); err != nil {
		return Probe{}, fmt.Errorf("could not inspect video: %w", err)
	}
	var probe Probe
	if err := json.Unmarshal(output.Bytes(), &probe); err != nil {
		return probe, errors.New("invalid video inspection response")
	}
	if _, err := probe.duration(); err != nil {
		return probe, err
	}
	if _, err := probe.video(); err != nil {
		return probe, err
	}
	return probe, nil
}
func (e Encoder) Convert(ctx context.Context, input, stage, profile string) (string, error) {
	before, err := e.probe(ctx, input)
	if err != nil {
		return "", err
	}
	video, _ := before.video()
	ext := ".mkv"
	if profile == "preserve" {
		candidate := strings.ToLower(filepath.Ext(input))
		if candidate == ".mp4" || candidate == ".mov" || candidate == ".m4v" {
			ext = candidate
		}
	}
	if profile == "compatible" {
		ext = ".mp4"
	}
	output := filepath.Join(stage, "output"+ext)
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-n", "-threads", "2"}
	transcode := profile == "compatible" && (video.Codec != "h264" || video.Pixel != "yuv420p")
	if transcode && (video.Transfer == "smpte2084" || video.Transfer == "arib-std-b67") {
		return "", errors.New("HDR video needs the original-codec profile; automatic tone mapping is not enabled")
	}
	if profile == "compatible" {
		for _, s := range before.Streams {
			if s.Type == "subtitle" {
				return "", errors.New("use the original-codec profile to preserve subtitles")
			}
		}
	}
	makeArgs := func(gpu bool) []string {
		a := append([]string{}, args...)
		if transcode && gpu {
			a = append(a, "-vaapi_device", "/dev/dri/renderD128")
		}
		a = append(a, "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,avi,mpegts,mpeg", "-i", input, "-map", fmt.Sprintf("0:%d", video.Index), "-map", "0:a?", "-map_metadata", "0", "-map_chapters", "0", "-c:v", "copy", "-c:a", "copy")
		if profile == "preserve" {
			a = append(a, "-map", "0:s?", "-c:s", "copy")
		}
		if transcode {
			if gpu {
				a = append(a, "-vf", "format=nv12,hwupload", "-c:v", "h264_vaapi", "-qp", "22")
			} else {
				a = append(a, "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p")
			}
		}
		if profile == "compatible" {
			for _, s := range before.Streams {
				if s.Type == "audio" && s.Codec != "aac" && s.Codec != "mp3" {
					a = append(a, "-c:a", "aac", "-b:a", "192k")
					break
				}
			}
			a = append(a, "-movflags", "+faststart")
		}
		return append(a, "-threads", "2", "-filter_threads", "1", "-fs", strconv.FormatInt(maxMediaBytes, 10), output)
	}
	run := func(gpu bool) error {
		cmd := command(ctx, e.FFmpeg, makeArgs(gpu)...)
		diagnostic := &boundedBuffer{limit: 8192}
		cmd.Stdout, cmd.Stderr = diagnostic, diagnostic
		return cmd.Run()
	}
	if err = run(e.GPU); err != nil && e.GPU && transcode && ctx.Err() == nil {
		if removeErr := os.Remove(output); removeErr != nil && !os.IsNotExist(removeErr) {
			return "", removeErr
		}
		err = run(false)
	}
	if err != nil {
		return "", fmt.Errorf("video processing failed: %w", err)
	}
	after, err := e.probe(ctx, output)
	if err != nil {
		return "", err
	}
	if err = validateOutput(before, after, profile); err != nil {
		return "", err
	}
	// Read all video/audio packets before publication. This validates the
	// container without an unnecessary full software decode on copy jobs.
	check := command(ctx, e.FFmpeg, "-nostdin", "-v", "error", "-xerror", "-threads", "2", "-protocol_whitelist", "file,pipe", "-i", output, "-map", "0:v:0", "-map", "0:a?", "-c", "copy", "-f", "null", "-")
	diagnostic := &boundedBuffer{limit: 8192}
	check.Stdout, check.Stderr = diagnostic, diagnostic
	if err = check.Run(); err != nil {
		return "", errors.New("processed video failed packet validation")
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() <= 0 || info.Size() >= maxMediaBytes {
		return "", errors.New("processed video exceeds its size limit")
	}
	return output, nil
}
func validateOutput(before, after Probe, profile string) error {
	oldDuration, err := before.duration()
	if err != nil {
		return err
	}
	duration, err := after.duration()
	if err != nil {
		return err
	}
	if math.Abs(duration-oldDuration) > math.Max(.5, oldDuration*.001) {
		return errors.New("processed duration differs from the source")
	}
	oldVideo, _ := before.video()
	video, err := after.video()
	if err != nil {
		return err
	}
	displaySize := func(s Stream) (int, int) {
		for _, data := range s.SideData {
			if data.Rotation%180 != 0 {
				return s.Height, s.Width
			}
		}
		return s.Width, s.Height
	}
	oldWidth, oldHeight := displaySize(oldVideo)
	width, height := displaySize(video)
	if oldWidth != width || oldHeight != height {
		return errors.New("processed dimensions differ from the source")
	}
	for _, kind := range []string{"audio", "subtitle"} {
		var original, result []string
		for _, s := range before.Streams {
			if s.Type == kind {
				original = append(original, s.Codec)
			}
		}
		for _, s := range after.Streams {
			if s.Type == kind {
				result = append(result, s.Codec)
			}
		}
		if len(original) != len(result) {
			return fmt.Errorf("processed %s stream count differs", kind)
		}
		if profile == "preserve" && strings.Join(original, ",") != strings.Join(result, ",") {
			return fmt.Errorf("processed %s codecs differ", kind)
		}
	}
	if profile == "preserve" && oldVideo.Codec != video.Codec {
		return errors.New("processed video codec differs")
	}
	if profile == "compatible" && (video.Codec != "h264" || video.Pixel != "yuv420p") {
		return errors.New("processed video does not match the playback profile")
	}
	return nil
}

func NewEncoder() Encoder {
	e := Encoder{FFmpeg: "ffmpeg", FFprobe: "ffprobe"}
	info, err := os.Stat("/dev/dri/renderD128")
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := command(ctx, e.FFmpeg, "-nostdin", "-v", "error", "-vaapi_device", "/dev/dri/renderD128", "-f", "lavfi", "-i", "color=s=64x64:d=0.1", "-vf", "format=nv12,hwupload", "-frames:v", "1", "-c:v", "h264_vaapi", "-f", "null", "-")
	e.GPU = cmd.Run() == nil
	return e
}
