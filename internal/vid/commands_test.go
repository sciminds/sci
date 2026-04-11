package vid

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildMuteArgs(t *testing.T) {
	t.Parallel()
	got := BuildMuteArgs("input.mp4", "out.mp4")
	want := []string{"-i", "input.mp4", "-c", "copy", "-an", "out.mp4"}
	assertArgs(t, got, want)
}

func TestBuildStripSubsArgs(t *testing.T) {
	t.Parallel()
	got := BuildStripSubsArgs("input.mp4", "out.mp4")
	want := []string{"-i", "input.mp4", "-c", "copy", "-sn", "out.mp4"}
	assertArgs(t, got, want)
}

func TestBuildSpeedArgs(t *testing.T) {
	t.Parallel()
	t.Run("basic 2x", func(t *testing.T) {
		got := BuildSpeedArgs("in.mp4", "out.mp4", 2.0, SpeedOpts{})
		assertContains(t, got, "-vf")
		assertContains(t, got, "-af")
		assertNotContains(t, got, "-an")
	})

	t.Run("no audio", func(t *testing.T) {
		got := BuildSpeedArgs("in.mp4", "out.mp4", 1.5, SpeedOpts{NoAudio: true})
		assertContains(t, got, "-an")
		assertNotContains(t, got, "-af")
	})

	t.Run("smooth", func(t *testing.T) {
		got := BuildSpeedArgs("in.mp4", "out.mp4", 2.0, SpeedOpts{Smooth: true})
		vf := findArgValue(got, "-vf")
		if !strings.Contains(vf, "minterpolate") {
			t.Errorf("expected minterpolate in vf filter, got %q", vf)
		}
	})

	t.Run("hw encoder", func(t *testing.T) {
		got := BuildSpeedArgs("in.mp4", "out.mp4", 1.5, SpeedOpts{HWEncoder: "h264_videotoolbox"})
		assertArgValue(t, got, "-c:v", "h264_videotoolbox")
	})
}

func TestBuildCutArgs(t *testing.T) {
	t.Parallel()
	t.Run("fast stream copy", func(t *testing.T) {
		got := BuildCutArgs("in.mp4", "out.mp4", 30, 60, CutOpts{})
		// -ss before -i for fast seek
		assertArgs(t, got[:4], []string{"-ss", "30", "-to", "60"})
		assertContains(t, got, "-c")
		assertContains(t, got, "copy")
	})

	t.Run("accurate re-encode", func(t *testing.T) {
		got := BuildCutArgs("in.mp4", "out.mp4", 30, 60, CutOpts{Accurate: true, HWEncoder: "h264_videotoolbox"})
		// -i before -ss for accuracy
		if got[0] != "-i" {
			t.Errorf("accurate mode should start with -i, got %q", got[0])
		}
		assertArgValue(t, got, "-c:v", "h264_videotoolbox")
	})

	t.Run("accurate fallback encoder", func(t *testing.T) {
		got := BuildCutArgs("in.mp4", "out.mp4", 30, 60, CutOpts{Accurate: true})
		assertArgValue(t, got, "-c:v", "libx264")
	})
}

func TestBuildResizeArgs(t *testing.T) {
	t.Parallel()
	t.Run("720p preset", func(t *testing.T) {
		got, err := BuildResizeArgs("in.mp4", "out.mp4", "720p", "")
		if err != nil {
			t.Fatal(err)
		}
		vf := findArgValue(got, "-vf")
		if !strings.Contains(vf, "1280:-2") {
			t.Errorf("720p should use 1280:-2, got %q", vf)
		}
	})

	t.Run("percentage", func(t *testing.T) {
		got, err := BuildResizeArgs("in.mp4", "out.mp4", "50%", "")
		if err != nil {
			t.Fatal(err)
		}
		vf := findArgValue(got, "-vf")
		if !strings.Contains(vf, "iw*0.5") {
			t.Errorf("50%% should use iw*0.5, got %q", vf)
		}
	})

	t.Run("explicit W:H", func(t *testing.T) {
		got, err := BuildResizeArgs("in.mp4", "out.mp4", "1920:", "")
		if err != nil {
			t.Fatal(err)
		}
		vf := findArgValue(got, "-vf")
		if !strings.Contains(vf, "1920:") {
			t.Errorf("explicit should pass through, got %q", vf)
		}
	})

	t.Run("invalid size", func(t *testing.T) {
		_, err := BuildResizeArgs("in.mp4", "out.mp4", "banana", "")
		if err == nil {
			t.Error("expected error for invalid size")
		}
	})
}

func TestBuildExtractAudioArgs(t *testing.T) {
	t.Parallel()
	t.Run("mp3 default", func(t *testing.T) {
		got, err := BuildExtractAudioArgs("in.mp4", "out.mp3", "mp3")
		if err != nil {
			t.Fatal(err)
		}
		assertContains(t, got, "-vn")
		assertContains(t, got, "libmp3lame")
	})

	t.Run("flac", func(t *testing.T) {
		got, err := BuildExtractAudioArgs("in.mp4", "out.flac", "flac")
		if err != nil {
			t.Fatal(err)
		}
		assertContains(t, got, "flac")
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := BuildExtractAudioArgs("in.mp4", "out.ogg", "ogg")
		if err == nil {
			t.Error("expected error for unsupported format")
		}
	})
}

func TestBuildConvertArgs(t *testing.T) {
	t.Parallel()
	t.Run("mp4 medium", func(t *testing.T) {
		got, err := BuildConvertArgs("in.webm", "out.mp4", "mp4", "medium", "")
		if err != nil {
			t.Fatal(err)
		}
		assertArgValue(t, got, "-crf", "23")
		assertContains(t, got, "-c:a")
	})

	t.Run("with hw encoder", func(t *testing.T) {
		got, err := BuildConvertArgs("in.webm", "out.mp4", "mp4", "high", "h264_videotoolbox")
		if err != nil {
			t.Fatal(err)
		}
		assertArgValue(t, got, "-c:v", "h264_videotoolbox")
		assertArgValue(t, got, "-crf", "18")
	})

	t.Run("invalid format", func(t *testing.T) {
		_, err := BuildConvertArgs("in.mp4", "out.avi", "avi", "medium", "")
		if err == nil {
			t.Error("expected error for unsupported format")
		}
	})

	t.Run("invalid quality", func(t *testing.T) {
		_, err := BuildConvertArgs("in.mp4", "out.mp4", "mp4", "ultra", "")
		if err == nil {
			t.Error("expected error for invalid quality")
		}
	})
}

func TestBuildGifArgs(t *testing.T) {
	t.Parallel()
	t.Run("defaults", func(t *testing.T) {
		got := BuildGifArgs("in.mp4", "out.gif", GifOpts{Width: 480, FPS: 12})
		assertContains(t, got, "-filter_complex")
		fc := findArgValue(got, "-filter_complex")
		if !strings.Contains(fc, "fps=12") {
			t.Errorf("expected fps=12 in filter, got %q", fc)
		}
		if !strings.Contains(fc, "scale=480") {
			t.Errorf("expected scale=480 in filter, got %q", fc)
		}
	})

	t.Run("with time range", func(t *testing.T) {
		got := BuildGifArgs("in.mp4", "out.gif", GifOpts{
			Width: 320, FPS: 10, Start: 5, End: 15,
		})
		assertArgValue(t, got, "-ss", "5")
		assertArgValue(t, got, "-to", "15")
	})

	t.Run("with duration", func(t *testing.T) {
		got := BuildGifArgs("in.mp4", "out.gif", GifOpts{
			Width: 480, FPS: 12, Duration: 3,
		})
		assertArgValue(t, got, "-t", "3")
	})
}

func TestBuildCompressArgs(t *testing.T) {
	t.Parallel()
	t.Run("quality preset", func(t *testing.T) {
		got, err := BuildCompressArgs("in.mp4", "out.mp4", CompressOpts{Quality: "medium"})
		if err != nil {
			t.Fatal(err)
		}
		assertArgValue(t, got, "-crf", "23")
		assertArgValue(t, got, "-c:a", "copy")
	})

	t.Run("explicit crf", func(t *testing.T) {
		got, err := BuildCompressArgs("in.mp4", "out.mp4", CompressOpts{CRF: 15})
		if err != nil {
			t.Fatal(err)
		}
		assertArgValue(t, got, "-crf", "15")
	})

	t.Run("hw encoder", func(t *testing.T) {
		got, err := BuildCompressArgs("in.mp4", "out.mp4", CompressOpts{Quality: "high", HWEncoder: "h264_videotoolbox"})
		if err != nil {
			t.Fatal(err)
		}
		assertArgValue(t, got, "-c:v", "h264_videotoolbox")
	})

	t.Run("invalid crf", func(t *testing.T) {
		_, err := BuildCompressArgs("in.mp4", "out.mp4", CompressOpts{CRF: 55})
		if err == nil {
			t.Error("expected error for CRF > 51")
		}
	})

	t.Run("invalid quality", func(t *testing.T) {
		_, err := BuildCompressArgs("in.mp4", "out.mp4", CompressOpts{Quality: "ultra"})
		if err == nil {
			t.Error("expected error for invalid quality")
		}
	})
}

// --- helpers ---

func assertArgs(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args length %d != %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func assertContains(t *testing.T, args []string, val string) {
	t.Helper()
	if !slices.Contains(args, val) {
		t.Errorf("expected args to contain %q, got %v", val, args)
	}
}

func assertNotContains(t *testing.T, args []string, val string) {
	t.Helper()
	if slices.Contains(args, val) {
		t.Errorf("expected args NOT to contain %q, got %v", val, args)
	}
}

func findArgValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func assertArgValue(t *testing.T, args []string, flag, want string) {
	t.Helper()
	got := findArgValue(args, flag)
	if got != want {
		t.Errorf("flag %s = %q, want %q (args: %v)", flag, got, want, args)
	}
}

// --- atempo ---

func TestBuildAtempo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		speed float64
		want  string
	}{
		{"normal 1.5x", 1.5, "atempo=1.5"},
		{"2x exactly", 2.0, "atempo=2"},
		{"0.5x exactly", 0.5, "atempo=0.5"},
		{"4x chains two", 4.0, "atempo=2.0,atempo=2"},
		{"0.25x chains two", 0.25, "atempo=0.5,atempo=0.5"},
		{"8x chains three", 8.0, "atempo=2.0,atempo=2.0,atempo=2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildAtempo(tt.speed)
			joined := strings.Join(got, ",")
			if joined != tt.want {
				t.Errorf("BuildAtempo(%v) = %q, want %q", tt.speed, joined, tt.want)
			}
		})
	}
}

func TestBuildAtempoInvalid(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Error("BuildAtempo(0) should panic")
		}
	}()
	BuildAtempo(0)
}
