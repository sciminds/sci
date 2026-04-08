package vid

// ffmpeg.go — low-level ffmpeg/ffprobe wrappers: binary detection, media
// probing (duration, codec, resolution), and file-size estimation.

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sciminds/cli/internal/ui"
)

// RequireFfmpeg checks that ffmpeg and ffprobe are on PATH.
func RequireFfmpeg() error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg is required — install with: brew install ffmpeg")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return fmt.Errorf("ffprobe is required — install with: brew install ffmpeg")
	}
	return nil
}

// RunFfmpeg executes ffmpeg with the given args.
// In dry-run mode it prints the command instead.
func RunFfmpeg(args []string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "%s\n", ui.TUI.Dim().Render("$ ffmpeg "+strings.Join(args, " ")))
		return nil
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	return nil
}

// ProbeInfo holds parsed ffprobe metadata.
type ProbeInfo struct {
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Codec    string  `json:"codec"`
	FPS      float64 `json:"fps"`
	Duration float64 `json:"duration"`
	Size     int64   `json:"size"`
	HasAudio bool    `json:"hasAudio"`
	HasSubs  bool    `json:"hasSubs"`
}

// Probe runs ffprobe and returns parsed video info.
func Probe(file string) (*ProbeInfo, error) {
	raw, err := ProbeJSON(file)
	if err != nil {
		return nil, err
	}

	data, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected ffprobe output")
	}

	info := &ProbeInfo{Codec: "unknown"}

	streams, _ := data["streams"].([]any)
	for _, s := range streams {
		stream, _ := s.(map[string]any)
		codecType, _ := stream["codec_type"].(string)
		switch codecType {
		case "video":
			if w, ok := stream["width"].(float64); ok {
				info.Width = int(w)
			}
			if h, ok := stream["height"].(float64); ok {
				info.Height = int(h)
			}
			if c, ok := stream["codec_name"].(string); ok {
				info.Codec = c
			}
			if fpsStr, ok := stream["r_frame_rate"].(string); ok {
				info.FPS = parseFraction(fpsStr)
			}
		case "audio":
			info.HasAudio = true
		case "subtitle":
			info.HasSubs = true
		}
	}

	if format, ok := data["format"].(map[string]any); ok {
		if d, ok := format["duration"].(string); ok {
			info.Duration, _ = strconv.ParseFloat(d, 64) //nolint:errcheck // best-effort from ffprobe
		}
		if s, ok := format["size"].(string); ok {
			info.Size, _ = strconv.ParseInt(s, 10, 64) //nolint:errcheck // best-effort from ffprobe
		}
	}

	return info, nil
}

// ProbeJSON runs ffprobe and returns the raw JSON output.
func ProbeJSON(file string) (any, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format", "-show_streams",
		file,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	var result any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("ffprobe JSON: %w", err)
	}
	return result, nil
}

// DetectHWEncoder checks if a VideoToolbox hardware encoder is available.
func DetectHWEncoder(codec string) string {
	vtEncoder := codec + "_videotoolbox"
	cmd := exec.Command("ffmpeg", "-encoders", "-hide_banner")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	if strings.Contains(string(out), vtEncoder) {
		return vtEncoder
	}
	return ""
}

func parseFraction(s string) float64 {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return 0
	}
	num, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	den, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || den == 0 {
		return 0
	}
	return math.Round(num/den*100) / 100
}
