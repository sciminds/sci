// Package vid wraps ffmpeg to provide friendly video/audio editing operations.
//
// Each operation has a Build*Args function that constructs the ffmpeg argument
// list (exported for testing) and is executed via [RunFfmpeg]. All operations
// support a --dry-run mode that prints the command without executing.
//
// Supported operations:
//
//   - Mute, StripSubs — stream-copy with tracks removed (fast, no re-encode)
//   - Speed — retimes video with optional motion interpolation and audio tempo
//   - Cut — trims by start/end time, with optional frame-accurate re-encoding
//   - Resize — scales to presets (720p, 1080p) or custom dimensions
//   - ExtractAudio — saves audio track as mp3/flac/wav/aac
//   - Convert — transcodes to mp4/hevc/webm/av1/mov with quality presets
//   - Gif — creates optimized GIFs with palette generation
//   - Compress — re-encodes with CRF control and reports file size savings
//
// Hardware acceleration (VideoToolbox on macOS) is detected via [DetectHWEncoder]
// and used automatically when available.
package vid

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"
)

// BuildMuteArgs returns ffmpeg args to remove the audio track (stream copy).
func BuildMuteArgs(input, output string) []string {
	return []string{"-i", input, "-c", "copy", "-an", output}
}

// BuildStripSubsArgs returns ffmpeg args to remove subtitle tracks (stream copy).
func BuildStripSubsArgs(input, output string) []string {
	return []string{"-i", input, "-c", "copy", "-sn", output}
}

// SpeedOpts holds options for speed change.
type SpeedOpts struct {
	NoAudio   bool
	Smooth    bool
	HWEncoder string
}

// BuildSpeedArgs returns ffmpeg args to change playback speed.
func BuildSpeedArgs(input, output string, speed float64, opts SpeedOpts) []string {
	vf := fmt.Sprintf("setpts=%g*PTS", 1/speed)
	if opts.Smooth {
		vf += ",minterpolate='fps=60'"
	}

	args := []string{"-i", input, "-vf", vf}

	if opts.NoAudio {
		args = append(args, "-an")
	} else {
		atempoFilters := BuildAtempo(speed)
		args = append(args, "-af", strings.Join(atempoFilters, ","))
	}

	if opts.HWEncoder != "" {
		args = append(args, "-c:v", opts.HWEncoder)
	}

	args = append(args, output)
	return args
}

// CutOpts holds options for cutting/trimming.
type CutOpts struct {
	Accurate  bool
	HWEncoder string
}

// BuildCutArgs returns ffmpeg args to trim a video segment.
func BuildCutArgs(input, output string, startSec, endSec float64, opts CutOpts) []string {
	start := formatSeconds(startSec)
	end := formatSeconds(endSec)

	if opts.Accurate {
		encoder := cmp.Or(opts.HWEncoder, "libx264")
		return []string{
			"-i", input,
			"-ss", start,
			"-to", end,
			"-c:v", encoder,
			"-c:a", "copy",
			output,
		}
	}

	// Fast mode: -ss before -i for stream copy
	return []string{"-ss", start, "-to", end, "-i", input, "-c", "copy", output}
}

// Resize presets mapping common names to ffmpeg scale expressions.
var resizePresets = map[string]string{
	"720p":  "1280:-2",
	"1080p": "1920:-2",
	"4k":    "3840:-2",
}

// BuildResizeArgs returns ffmpeg args to scale a video.
func BuildResizeArgs(input, output, size, hwEncoder string) ([]string, error) {
	var scaleExpr string

	lower := strings.ToLower(size)
	if preset, ok := resizePresets[lower]; ok {
		scaleExpr = preset
	} else if strings.HasSuffix(size, "%") {
		pct, err := strconv.ParseFloat(strings.TrimSuffix(size, "%"), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid percentage: %s", size)
		}
		factor := pct / 100
		scaleExpr = fmt.Sprintf("iw*%g:ih*%g", factor, factor)
	} else if strings.Contains(size, ":") {
		scaleExpr = size
	} else {
		return nil, fmt.Errorf("invalid size: %s (use 720p, 1080p, 4k, 50%%, or W:H)", size)
	}

	encoder := cmp.Or(hwEncoder, "libx264")

	return []string{
		"-i", input,
		"-vf", fmt.Sprintf("scale=%s:flags=lanczos", scaleExpr),
		"-c:v", encoder,
		"-c:a", "copy",
		output,
	}, nil
}

// Audio codec configuration.
type audioCodecConf struct {
	codec string // may contain extra args like "libmp3lame -q:a 2"
	ext   string
}

var audioCodecs = map[string]audioCodecConf{
	"mp3":  {codec: "libmp3lame -q:a 2", ext: ".mp3"},
	"flac": {codec: "flac", ext: ".flac"},
	"wav":  {codec: "pcm_s16le", ext: ".wav"},
	"aac":  {codec: "aac -b:a 192k", ext: ".aac"},
}

// AudioExt returns the file extension for a supported audio format.
func AudioExt(format string) (string, error) {
	conf, ok := audioCodecs[format]
	if !ok {
		return "", fmt.Errorf("unsupported audio format: %s (use mp3, flac, wav, or aac)", format)
	}
	return conf.ext, nil
}

// BuildExtractAudioArgs returns ffmpeg args to extract the audio track.
func BuildExtractAudioArgs(input, output, format string) ([]string, error) {
	conf, ok := audioCodecs[format]
	if !ok {
		return nil, fmt.Errorf("unsupported audio format: %s (use mp3, flac, wav, or aac)", format)
	}

	codecParts := strings.Fields(conf.codec)
	args := []string{"-i", input, "-vn", "-c:a"}
	args = append(args, codecParts...)
	args = append(args, output)
	return args, nil
}

// Video format configuration.
type convertConf struct {
	encoder string
	ext     string
	hwCodec string // "h264" or "hevc" if hw accel is possible
}

var formatMap = map[string]convertConf{
	"mp4":  {encoder: "libx264", ext: ".mp4", hwCodec: "h264"},
	"hevc": {encoder: "libx265", ext: ".mp4", hwCodec: "hevc"},
	"webm": {encoder: "libvpx-vp9", ext: ".webm"},
	"av1":  {encoder: "libaom-av1", ext: ".mp4"},
	"mov":  {encoder: "libx264", ext: ".mov", hwCodec: "h264"},
}

var qualityCRF = map[string]int{
	"high":   18,
	"medium": 23,
	"low":    28,
}

// ConvertExt returns the file extension for a supported video format.
func ConvertExt(format string) (string, error) {
	conf, ok := formatMap[format]
	if !ok {
		return "", fmt.Errorf("unsupported format: %s (use mp4, hevc, webm, av1, or mov)", format)
	}
	return conf.ext, nil
}

// BuildConvertArgs returns ffmpeg args to transcode a video.
func BuildConvertArgs(input, output, format, quality, hwEncoder string) ([]string, error) {
	conf, ok := formatMap[format]
	if !ok {
		return nil, fmt.Errorf("unsupported format: %s (use mp4, hevc, webm, av1, or mov)", format)
	}

	crf, ok := qualityCRF[quality]
	if !ok {
		return nil, fmt.Errorf("invalid quality: %s (use high, medium, or low)", quality)
	}

	encoder := conf.encoder
	if hwEncoder != "" {
		encoder = hwEncoder
	}

	return []string{
		"-i", input,
		"-c:v", encoder,
		"-crf", strconv.Itoa(crf),
		"-c:a", "aac",
		output,
	}, nil
}

// GifOpts holds options for GIF creation.
type GifOpts struct {
	Start    float64
	End      float64
	Duration float64
	Width    int
	FPS      int
}

// BuildGifArgs returns ffmpeg args to create an optimized GIF.
func BuildGifArgs(input, output string, opts GifOpts) []string {
	var args []string

	if opts.Start > 0 {
		args = append(args, "-ss", formatSeconds(opts.Start))
	}
	if opts.End > 0 {
		args = append(args, "-to", formatSeconds(opts.End))
	}
	if opts.Duration > 0 {
		args = append(args, "-t", formatSeconds(opts.Duration))
	}

	filterComplex := fmt.Sprintf(
		"fps=%d,scale=%d:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse=dither=bayer",
		opts.FPS, opts.Width,
	)

	args = append(args, "-i", input, "-filter_complex", filterComplex, output)
	return args
}

// CompressOpts holds options for video compression.
type CompressOpts struct {
	Quality   string
	CRF       int
	HWEncoder string
}

// BuildCompressArgs returns ffmpeg args to re-encode at a quality preset.
func BuildCompressArgs(input, output string, opts CompressOpts) ([]string, error) {
	var crf int

	if opts.CRF > 0 {
		if opts.CRF > 51 {
			return nil, fmt.Errorf("CRF must be 0-51")
		}
		crf = opts.CRF
	} else {
		quality := opts.Quality
		if quality == "" {
			quality = "medium"
		}
		mapped, ok := qualityCRF[quality]
		if !ok {
			return nil, fmt.Errorf("invalid quality: %s (use high, medium, or low)", quality)
		}
		crf = mapped
	}

	encoder := cmp.Or(opts.HWEncoder, "libx264")

	return []string{
		"-i", input,
		"-c:v", encoder,
		"-crf", strconv.Itoa(crf),
		"-c:a", "copy",
		output,
	}, nil
}

// formatSeconds converts a float to a string, using integer form when possible.
func formatSeconds(s float64) string {
	if s == float64(int(s)) {
		return strconv.Itoa(int(s))
	}
	return strconv.FormatFloat(s, 'f', -1, 64)
}

// BuildAtempo builds chained atempo filter values for ffmpeg.
// ffmpeg's atempo filter only accepts values in [0.5, 2.0],
// so speeds outside that range require chaining multiple filters.
func BuildAtempo(speed float64) []string {
	if speed <= 0 {
		panic("speed must be positive")
	}
	var filters []string
	remaining := speed
	for remaining > 2.0 {
		filters = append(filters, "atempo=2.0")
		remaining /= 2.0
	}
	for remaining < 0.5 {
		filters = append(filters, "atempo=0.5")
		remaining /= 0.5
	}
	filters = append(filters, fmt.Sprintf("atempo=%g", remaining))
	return filters
}
