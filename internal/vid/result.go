package vid

// result.go — [cmdutil.Result] implementations (JSON + Human output) for each
// vid subcommand: info, trim, speed, compress, and extract-audio.

import (
	"fmt"

	"github.com/sciminds/cli/internal/ui"
)

// InfoResult implements cmdutil.Result for the info subcommand.
type InfoResult struct {
	File string    `json:"file"`
	Info ProbeInfo `json:"info"`
}

// JSON implements cmdutil.Result.
func (r InfoResult) JSON() any { return r }

// Human implements cmdutil.Result.
func (r InfoResult) Human() string {
	sizeMB := float64(r.Info.Size) / 1024 / 1024
	hasAudio := "no"
	if r.Info.HasAudio {
		hasAudio = "yes"
	}
	hasSubs := "no"
	if r.Info.HasSubs {
		hasSubs = "yes"
	}

	return fmt.Sprintf(
		"%s       %s\n%s %dx%d\n%s      %s\n%s        %g\n%s   %s\n%s       %.1f MB\n%s      %s\n%s  %s\n",
		ui.TUI.Bold().Render("File:"), r.File,
		ui.TUI.Bold().Render("Resolution:"), r.Info.Width, r.Info.Height,
		ui.TUI.Bold().Render("Codec:"), r.Info.Codec,
		ui.TUI.Bold().Render("FPS:"), r.Info.FPS,
		ui.TUI.Bold().Render("Duration:"), FormatTime(r.Info.Duration),
		ui.TUI.Bold().Render("Size:"), sizeMB,
		ui.TUI.Bold().Render("Audio:"), hasAudio,
		ui.TUI.Bold().Render("Subtitles:"), hasSubs,
	)
}
