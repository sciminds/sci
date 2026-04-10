package selfupdate

import (
	"fmt"
	"os"

	"github.com/sciminds/cli/internal/version"
)

// CheckBackground performs a live update check. It returns a user-facing
// message if an update is available, or "" if not (or on error/skip).
// Intended to run in a goroutine — the caller controls the timeout.
func CheckBackground() string {
	if version.Commit == "unknown" {
		return ""
	}
	if os.Getenv("SCI_NO_UPDATE_CHECK") != "" {
		return ""
	}

	result := Check()
	if result.Available {
		return fmt.Sprintf("Update available: %s → run: sci update", ShortSHA(result.LatestSHA))
	}
	return ""
}
