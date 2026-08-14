package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/uikit"
	"github.com/sciminds/sci/internal/vid"
	"github.com/urfave/cli/v3"
)

// --- vid command ---

func vidCommand() *cli.Command {
	return &cli.Command{
		Name:        "vid",
		Usage:       "Common video editing operations (trim, resize, mute, etc)",
		Description: "$ sci vid info lecture.mp4\n$ sci vid cut lecture.mp4 0:30 1:00\n$ sci vid gif demo.mov --start 0:05 --duration 3",
		Category:    "Commands",
		// Shared flags — propagate to all subcommands. lint:no-local
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "custom output path"},  // lint:no-local
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "overwrite without asking"}, // lint:no-local
			&cli.BoolFlag{Name: "dry-run", Usage: "print ffmpeg command without executing"},       // lint:no-local
		},
		Commands: []*cli.Command{
			vidInfoCommand(),
			vidMuteCommand(),
			vidStripSubsCommand(),
			vidSpeedCommand(),
			vidCutCommand(),
			vidResizeCommand(),
			vidExtractAudioCommand(),
			vidConvertCommand(),
			vidGifCommand(),
			vidCompressCommand(),
		},
	}
}

func vidInfoCommand() *cli.Command {
	return &cli.Command{
		Name:        "info",
		Usage:       "Show video info (resolution, duration, codec, fps, size)",
		Description: "$ sci vid info lecture.mp4",
		ArgsUsage:   "<file>",
		Action:      runVidInfo,
	}
}

func vidMuteCommand() *cli.Command {
	return &cli.Command{
		Name:        "mute",
		Usage:       "Remove audio from a video",
		Description: "$ sci vid mute recording.mp4",
		ArgsUsage:   "<file>",
		Action:      runVidMute,
	}
}

func vidStripSubsCommand() *cli.Command {
	return &cli.Command{
		Name:        "strip-subs",
		Usage:       "Remove subtitles from a video",
		Description: "$ sci vid strip-subs movie.mkv",
		ArgsUsage:   "<file>",
		Action:      runVidStripSubs,
	}
}

func vidSpeedCommand() *cli.Command {
	return &cli.Command{
		Name:        "speed",
		Usage:       "Change playback speed (e.g. 2 = 2x faster)",
		Description: "$ sci vid speed lecture.mp4 2\n$ sci vid speed demo.mp4 0.5 --smooth",
		ArgsUsage:   "<file> <multiplier>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "no-audio", Usage: "drop audio track", Local: true},
			&cli.BoolFlag{Name: "smooth", Usage: "use motion interpolation (slower)", Local: true},
		},
		Action: runVidSpeed,
	}
}

func vidCutCommand() *cli.Command {
	return &cli.Command{
		Name:        "cut",
		Usage:       "Trim a segment (e.g. 0:30 1:00)",
		Description: "$ sci vid cut lecture.mp4 0:30 1:00\n$ sci vid cut lecture.mp4 0:30 1:00 --accurate",
		ArgsUsage:   "<file> <start> <end>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "accurate", Aliases: []string{"a"}, Usage: "re-encode for frame-accurate cuts", Local: true},
		},
		Action: runVidCut,
	}
}

func vidResizeCommand() *cli.Command {
	return &cli.Command{
		Name:        "resize",
		Usage:       "Scale video (720p, 1080p, 4k, 50%, W:H)",
		Description: "$ sci vid resize lecture.mp4 720p\n$ sci vid resize demo.mov 50%",
		ArgsUsage:   "<file> <size>",
		Action:      runVidResize,
	}
}

func vidExtractAudioCommand() *cli.Command {
	return &cli.Command{
		Name:        "extract-audio",
		Usage:       "Extract audio track to file",
		Description: "$ sci vid extract-audio lecture.mp4\n$ sci vid extract-audio lecture.mp4 -f flac",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Usage: "audio format (mp3, flac, wav, aac)", Value: "mp3", Local: true},
		},
		Action: runVidExtractAudio,
	}
}

func vidConvertCommand() *cli.Command {
	return &cli.Command{
		Name:        "convert",
		Usage:       "Convert to another format (MP4, WebM, etc.)",
		Description: "$ sci vid convert recording.mov\n$ sci vid convert lecture.mp4 -f webm",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Aliases: []string{"f"}, Usage: "target format (mp4, hevc, webm, av1, mov)", Value: "mp4", Local: true},
			&cli.StringFlag{Name: "quality", Aliases: []string{"q"}, Usage: "quality preset (high, medium, low)", Value: "medium", Local: true},
		},
		Action: runVidConvert,
	}
}

func vidGifCommand() *cli.Command {
	return &cli.Command{
		Name:        "gif",
		Usage:       "Convert to optimized GIF",
		Description: "$ sci vid gif demo.mov\n$ sci vid gif demo.mov --start 0:05 --duration 3",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "start", Usage: "start time (e.g. 0:30 or 90)", Local: true},
			&cli.StringFlag{Name: "end", Usage: "end time (e.g. 1:00 or 120)", Local: true},
			&cli.StringFlag{Name: "duration", Usage: "duration in seconds (e.g. 3 or 5.5)", Local: true},
			&cli.IntFlag{Name: "width", Aliases: []string{"w"}, Usage: "gif width in pixels", Value: 480, Local: true},
			&cli.IntFlag{Name: "fps", Usage: "frames per second", Value: 12, Local: true},
		},
		Action: runVidGif,
	}
}

func vidCompressCommand() *cli.Command {
	return &cli.Command{
		Name:        "compress",
		Usage:       "Shrink a video file (reduce file size)",
		Description: "$ sci vid compress lecture.mp4\n$ sci vid compress lecture.mp4 -q high",
		ArgsUsage:   "<file>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "quality", Aliases: []string{"q"}, Usage: "quality preset (high, medium, low)", Value: "medium", Local: true},
			&cli.IntFlag{Name: "crf", Usage: "manual CRF value (0-51, lower = better)", Local: true},
		},
		Action: runVidCompress,
	}
}

// --- vid helpers ---

func vidRequireFile(file string) error {
	if _, err := os.Stat(file); err != nil {
		return fmt.Errorf("file not found: %s", file)
	}
	return vid.RequireFfmpeg()
}

// vidConfirmOverwrite asks before clobbering an existing output file. yes is
// the value of the shared --yes flag, passed in by the caller.
func vidConfirmOverwrite(out string, yes bool) error {
	if yes {
		return nil
	}
	if _, err := os.Stat(out); err != nil {
		return nil // file doesn't exist, no need to confirm
	}
	return cmdutil.Confirm(fmt.Sprintf("%s already exists. Overwrite?", out))
}

func vidRequireArgs(cmd *cli.Command, n int) ([]string, error) {
	args := cmd.Args().Slice()
	if len(args) != n {
		return nil, cmdutil.UsageErrorf(cmd, "expected %d argument(s), got %d", n, len(args))
	}
	return args, nil
}

// --- vid runners ---

func runVidInfo(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 1)
	if err != nil {
		return err
	}
	file := args[0]
	if err := vidRequireFile(file); err != nil {
		return err
	}

	if cmdutil.IsJSON(cmd) {
		raw, err := vid.ProbeJSON(file)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(raw)
	}

	info, err := vid.Probe(file)
	if err != nil {
		return err
	}
	cmdutil.Output(cmd, vid.InfoResult{File: file, Info: *info})
	return nil
}

func runVidMute(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 1)
	if err != nil {
		return err
	}
	file := args[0]
	if err := vidRequireFile(file); err != nil {
		return err
	}
	out := cmd.String("output")
	if out == "" {
		out = vid.OutputPath(file, "muted", "")
	}
	if err := vidConfirmOverwrite(out, cmd.Bool("yes")); err != nil {
		if errors.Is(err, cmdutil.ErrCancelled) {
			return nil
		}
		return err
	}
	dryRun := cmd.Bool("dry-run")
	if err := vid.RunFfmpeg(vid.BuildMuteArgs(file, out), dryRun); err != nil {
		return err
	}
	if !dryRun {
		uikit.OK(out)
	}
	return nil
}

func runVidStripSubs(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 1)
	if err != nil {
		return err
	}
	file := args[0]
	if err := vidRequireFile(file); err != nil {
		return err
	}
	out := cmd.String("output")
	if out == "" {
		out = vid.OutputPath(file, "nosubs", "")
	}
	if err := vidConfirmOverwrite(out, cmd.Bool("yes")); err != nil {
		if errors.Is(err, cmdutil.ErrCancelled) {
			return nil
		}
		return err
	}
	dryRun := cmd.Bool("dry-run")
	if err := vid.RunFfmpeg(vid.BuildStripSubsArgs(file, out), dryRun); err != nil {
		return err
	}
	if !dryRun {
		uikit.OK(out)
	}
	return nil
}

func runVidSpeed(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 2)
	if err != nil {
		return err
	}
	file := args[0]
	if err := vidRequireFile(file); err != nil {
		return err
	}

	speed, err := strconv.ParseFloat(args[1], 64)
	if err != nil || speed <= 0 {
		return fmt.Errorf("invalid speed multiplier: %s", args[1])
	}

	suffix := fmt.Sprintf("%gx", speed)
	out := cmd.String("output")
	if out == "" {
		out = vid.OutputPath(file, suffix, "")
	}
	if err := vidConfirmOverwrite(out, cmd.Bool("yes")); err != nil {
		if errors.Is(err, cmdutil.ErrCancelled) {
			return nil
		}
		return err
	}

	opts := vid.SpeedOpts{
		NoAudio:   cmd.Bool("no-audio"),
		Smooth:    cmd.Bool("smooth"),
		HWEncoder: vid.DetectHWEncoder("h264"),
	}
	dryRun := cmd.Bool("dry-run")
	if err := vid.RunFfmpeg(vid.BuildSpeedArgs(file, out, speed, opts), dryRun); err != nil {
		return err
	}
	if !dryRun {
		uikit.OK(out)
	}
	return nil
}

func runVidCut(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 3)
	if err != nil {
		return err
	}
	file := args[0]
	if err := vidRequireFile(file); err != nil {
		return err
	}

	startSec, err := vid.ParseTime(args[1])
	if err != nil {
		return err
	}
	endSec, err := vid.ParseTime(args[2])
	if err != nil {
		return err
	}

	suffix := fmt.Sprintf("%s-%s", vid.FormatTimeFilename(startSec), vid.FormatTimeFilename(endSec))
	out := cmd.String("output")
	if out == "" {
		out = vid.OutputPath(file, suffix, "")
	}
	if err := vidConfirmOverwrite(out, cmd.Bool("yes")); err != nil {
		if errors.Is(err, cmdutil.ErrCancelled) {
			return nil
		}
		return err
	}

	accurate := cmd.Bool("accurate")
	opts := vid.CutOpts{Accurate: accurate}
	if accurate {
		opts.HWEncoder = vid.DetectHWEncoder("h264")
	}
	dryRun := cmd.Bool("dry-run")
	if err := vid.RunFfmpeg(vid.BuildCutArgs(file, out, startSec, endSec, opts), dryRun); err != nil {
		return err
	}
	if !dryRun {
		uikit.OK(out)
	}
	return nil
}

func runVidResize(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 2)
	if err != nil {
		return err
	}
	file := args[0]
	size := args[1]
	if err := vidRequireFile(file); err != nil {
		return err
	}

	out := cmd.String("output")
	if out == "" {
		suffix := strings.ToLower(size)
		suffix = strings.ReplaceAll(suffix, "%", "pct")
		suffix = strings.ReplaceAll(suffix, ":", "x")
		out = vid.OutputPath(file, suffix, "")
	}
	if err := vidConfirmOverwrite(out, cmd.Bool("yes")); err != nil {
		if errors.Is(err, cmdutil.ErrCancelled) {
			return nil
		}
		return err
	}

	ffArgs, err := vid.BuildResizeArgs(file, out, size, vid.DetectHWEncoder("h264"))
	if err != nil {
		return err
	}
	dryRun := cmd.Bool("dry-run")
	if err := vid.RunFfmpeg(ffArgs, dryRun); err != nil {
		return err
	}
	if !dryRun {
		uikit.OK(out)
	}
	return nil
}

func runVidExtractAudio(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 1)
	if err != nil {
		return err
	}
	file := args[0]
	if err := vidRequireFile(file); err != nil {
		return err
	}

	format := cmd.String("format")
	ext, err := vid.AudioExt(format)
	if err != nil {
		return err
	}
	out := cmd.String("output")
	if out == "" {
		out = vid.OutputPath(file, "", ext)
	}
	if err := vidConfirmOverwrite(out, cmd.Bool("yes")); err != nil {
		if errors.Is(err, cmdutil.ErrCancelled) {
			return nil
		}
		return err
	}

	ffArgs, err := vid.BuildExtractAudioArgs(file, out, format)
	if err != nil {
		return err
	}
	dryRun := cmd.Bool("dry-run")
	if err := vid.RunFfmpeg(ffArgs, dryRun); err != nil {
		return err
	}
	if !dryRun {
		uikit.OK(out)
	}
	return nil
}

func runVidConvert(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 1)
	if err != nil {
		return err
	}
	file := args[0]
	if err := vidRequireFile(file); err != nil {
		return err
	}

	format := cmd.String("format")
	ext, err := vid.ConvertExt(format)
	if err != nil {
		return err
	}
	out := cmd.String("output")
	if out == "" {
		out = vid.OutputPath(file, format, ext)
	}
	if err := vidConfirmOverwrite(out, cmd.Bool("yes")); err != nil {
		if errors.Is(err, cmdutil.ErrCancelled) {
			return nil
		}
		return err
	}

	hwEncoder := ""
	conf := map[string]string{"mp4": "h264", "hevc": "hevc", "mov": "h264"}
	if hwCodec, ok := conf[format]; ok {
		hwEncoder = vid.DetectHWEncoder(hwCodec)
	}

	ffArgs, err := vid.BuildConvertArgs(file, out, format, cmd.String("quality"), hwEncoder)
	if err != nil {
		return err
	}
	dryRun := cmd.Bool("dry-run")
	if err := vid.RunFfmpeg(ffArgs, dryRun); err != nil {
		return err
	}
	if !dryRun {
		uikit.OK(out)
	}
	return nil
}

func runVidGif(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 1)
	if err != nil {
		return err
	}
	file := args[0]
	if err := vidRequireFile(file); err != nil {
		return err
	}

	out := cmd.String("output")
	if out == "" {
		out = vid.OutputPath(file, "gif", ".gif")
	}
	if err := vidConfirmOverwrite(out, cmd.Bool("yes")); err != nil {
		if errors.Is(err, cmdutil.ErrCancelled) {
			return nil
		}
		return err
	}

	opts := vid.GifOpts{Width: cmd.Int("width"), FPS: cmd.Int("fps")}
	if start := cmd.String("start"); start != "" {
		s, err := vid.ParseTime(start)
		if err != nil {
			return err
		}
		opts.Start = s
	}
	if end := cmd.String("end"); end != "" {
		s, err := vid.ParseTime(end)
		if err != nil {
			return err
		}
		opts.End = s
	}
	if duration := cmd.String("duration"); duration != "" {
		s, err := vid.ParseTime(duration)
		if err != nil {
			return err
		}
		opts.Duration = s
	}

	dryRun := cmd.Bool("dry-run")
	if err := vid.RunFfmpeg(vid.BuildGifArgs(file, out, opts), dryRun); err != nil {
		return err
	}
	if !dryRun {
		uikit.OK(out)
	}
	return nil
}

func runVidCompress(_ context.Context, cmd *cli.Command) error {
	args, err := vidRequireArgs(cmd, 1)
	if err != nil {
		return err
	}
	file := args[0]
	if err := vidRequireFile(file); err != nil {
		return err
	}

	out := cmd.String("output")
	if out == "" {
		out = vid.OutputPath(file, "compressed", "")
	}
	if err := vidConfirmOverwrite(out, cmd.Bool("yes")); err != nil {
		if errors.Is(err, cmdutil.ErrCancelled) {
			return nil
		}
		return err
	}

	opts := vid.CompressOpts{
		Quality:   cmd.String("quality"),
		CRF:       cmd.Int("crf"),
		HWEncoder: vid.DetectHWEncoder("h264"),
	}
	ffArgs, err := vid.BuildCompressArgs(file, out, opts)
	if err != nil {
		return err
	}
	dryRun := cmd.Bool("dry-run")
	if err := vid.RunFfmpeg(ffArgs, dryRun); err != nil {
		return err
	}
	if !dryRun {
		origInfo, _ := os.Stat(file)
		newInfo, _ := os.Stat(out)
		if origInfo != nil && newInfo != nil {
			pct := (1 - float64(newInfo.Size())/float64(origInfo.Size())) * 100
			uikit.OK(out + " " + uikit.TUI.Dim().Render(fmt.Sprintf("(%.0f%% smaller)", pct)))
		} else {
			uikit.OK(out)
		}
	}
	return nil
}
