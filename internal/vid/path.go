package vid

// path.go — output file path generation with descriptive suffixes (e.g.
// "video_trimmed.mp4") and automatic collision avoidance.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OutputPath generates an output file path with a descriptive suffix and collision avoidance.
// If newExt is empty, the original extension is kept.
// If suffix is empty, no suffix is added (useful when changing extension only).
func OutputPath(input, suffix, newExt string) string {
	dir := filepath.Dir(input)
	ext := filepath.Ext(input)
	base := strings.TrimSuffix(filepath.Base(input), ext)

	if newExt != "" {
		ext = newExt
	}

	if suffix == "" {
		return filepath.Join(dir, base+ext)
	}

	candidate := filepath.Join(dir, fmt.Sprintf("%s_%s%s", base, suffix, ext))
	i := 1
	for fileExists(candidate) {
		candidate = filepath.Join(dir, fmt.Sprintf("%s_%s_%d%s", base, suffix, i, ext))
		i++
	}
	return candidate
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
