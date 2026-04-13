package uikit

// quiet.go — global toggle for non-interactive (--json) mode.
// When quiet, spinners are suppressed, progress bars are skipped,
// and the work function runs directly with status printed to stderr.

var quiet bool

// SetQuiet enables or disables quiet (non-interactive) mode.
// Called from the root command's Before hook when --json is set.
func SetQuiet(q bool) { quiet = q }

// IsQuiet reports whether quiet mode is active.
func IsQuiet() bool { return quiet }
