package vid

import (
	"strings"
	"testing"
)

func TestAudioExt(t *testing.T) {
	tests := []struct {
		format  string
		wantExt string
	}{
		{"mp3", ".mp3"},
		{"flac", ".flac"},
		{"wav", ".wav"},
		{"aac", ".aac"},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got, err := AudioExt(tt.format)
			if err != nil {
				t.Fatalf("AudioExt(%q) returned unexpected error: %v", tt.format, err)
			}
			if got != tt.wantExt {
				t.Errorf("AudioExt(%q) = %q, want %q", tt.format, got, tt.wantExt)
			}
		})
	}

	t.Run("invalid format", func(t *testing.T) {
		_, err := AudioExt("ogg")
		if err == nil {
			t.Error("AudioExt(\"ogg\") expected error, got nil")
		}
	})
}

func TestConvertExt(t *testing.T) {
	tests := []struct {
		format  string
		wantExt string
	}{
		{"mp4", ".mp4"},
		{"hevc", ".mp4"},
		{"webm", ".webm"},
		{"av1", ".mp4"},
		{"mov", ".mov"},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got, err := ConvertExt(tt.format)
			if err != nil {
				t.Fatalf("ConvertExt(%q) returned unexpected error: %v", tt.format, err)
			}
			if got != tt.wantExt {
				t.Errorf("ConvertExt(%q) = %q, want %q", tt.format, got, tt.wantExt)
			}
		})
	}

	t.Run("invalid format", func(t *testing.T) {
		_, err := ConvertExt("avi")
		if err == nil {
			t.Error("ConvertExt(\"avi\") expected error, got nil")
		}
	})
}

func TestInfoResultHuman(t *testing.T) {
	r := InfoResult{
		File: "video.mp4",
		Info: ProbeInfo{
			Width:    1920,
			Height:   1080,
			Codec:    "h264",
			FPS:      29.97,
			Duration: 120.5,
			Size:     10 * 1024 * 1024,
			HasAudio: true,
			HasSubs:  false,
		},
	}
	out := r.Human()

	for _, want := range []string{"video.mp4", "1920", "1080", "h264", "yes", "no"} {
		if !strings.Contains(out, want) {
			t.Errorf("InfoResult.Human() missing %q in output:\n%s", want, out)
		}
	}
}

func TestInfoResultHumanNoAudioNoSubs(t *testing.T) {
	r := InfoResult{
		File: "clip.mov",
		Info: ProbeInfo{
			Codec:    "hevc",
			HasAudio: false,
			HasSubs:  false,
		},
	}
	out := r.Human()
	if !strings.Contains(out, "clip.mov") {
		t.Errorf("InfoResult.Human() missing filename in output:\n%s", out)
	}
	// Both audio and subtitles should be "no"
	noCount := strings.Count(out, "no")
	if noCount < 2 {
		t.Errorf("InfoResult.Human() expected at least 2 'no' (audio + subs), got %d in:\n%s", noCount, out)
	}
}

func TestSimpleResultHuman(t *testing.T) {
	r := SimpleResult{Output: "/tmp/output.mp4"}
	out := r.Human()
	if !strings.Contains(out, "/tmp/output.mp4") {
		t.Errorf("SimpleResult.Human() missing output path in:\n%s", out)
	}
	// Should end with newline
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("SimpleResult.Human() should end with newline")
	}
}

func TestCompressResultHuman(t *testing.T) {
	r := CompressResult{
		Output:     "/tmp/compressed.mp4",
		OrigSize:   100 * 1024 * 1024,
		NewSize:    70 * 1024 * 1024,
		SavingsPct: 30.0,
	}
	out := r.Human()
	if !strings.Contains(out, "/tmp/compressed.mp4") {
		t.Errorf("CompressResult.Human() missing output path in:\n%s", out)
	}
	if !strings.Contains(out, "30%") {
		t.Errorf("CompressResult.Human() missing savings percentage in:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("CompressResult.Human() should end with newline")
	}
}
