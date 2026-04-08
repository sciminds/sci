package vid

// time.go — timestamp parsing for ffmpeg: plain seconds, HH:MM:SS, and 1h2m3s
// formats, plus duration formatting for human-readable output.

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	plainRe = regexp.MustCompile(`^\d+(\.\d+)?$`)
	colonRe = regexp.MustCompile(`^(?:(\d+):)?(\d+):(\d+(?:\.\d+)?)$`)
	hmsRe   = regexp.MustCompile(`^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s)?$`)
)

// ParseTime converts flexible time strings to seconds.
// Accepts: "90", "1:30", "01:30:00", "1h30m15s".
func ParseTime(input string) (float64, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, fmt.Errorf("empty time string")
	}

	if plainRe.MatchString(input) {
		return strconv.ParseFloat(input, 64)
	}

	// Errors from Atoi/ParseFloat are safe to ignore below: the regex
	// capture groups guarantee the matched substrings are valid numbers.
	if m := colonRe.FindStringSubmatch(input); m != nil {
		h := 0
		if m[1] != "" {
			h, _ = strconv.Atoi(m[1]) //nolint:errcheck // regex-validated digits
		}
		min, _ := strconv.Atoi(m[2])           //nolint:errcheck // regex-validated digits
		sec, _ := strconv.ParseFloat(m[3], 64) //nolint:errcheck // regex-validated digits
		return float64(h)*3600 + float64(min)*60 + sec, nil
	}

	if m := hmsRe.FindStringSubmatch(input); m != nil && (m[1] != "" || m[2] != "" || m[3] != "") {
		h := 0
		if m[1] != "" {
			h, _ = strconv.Atoi(m[1]) //nolint:errcheck // regex-validated digits
		}
		min := 0
		if m[2] != "" {
			min, _ = strconv.Atoi(m[2]) //nolint:errcheck // regex-validated digits
		}
		sec := 0.0
		if m[3] != "" {
			sec, _ = strconv.ParseFloat(m[3], 64) //nolint:errcheck // regex-validated digits
		}
		return float64(h)*3600 + float64(min)*60 + sec, nil
	}

	return 0, fmt.Errorf("cannot parse time: %q", input)
}

// FormatTime converts seconds to "M:SS" or "H:MM:SS".
func FormatTime(secs float64) string {
	total := int(math.Floor(secs))
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// FormatTimeFilename converts seconds to a filename-safe format like "1m30s" or "1h01m01s".
func FormatTimeFilename(secs float64) string {
	total := int(math.Floor(secs))
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	return fmt.Sprintf("%dm%02ds", m, s)
}
