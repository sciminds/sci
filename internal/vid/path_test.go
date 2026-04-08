package vid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		suffix string
		newExt string
		want   string
	}{
		{
			name:   "basic suffix",
			input:  "/tmp/video.mp4",
			suffix: "muted",
			want:   "/tmp/video_muted.mp4",
		},
		{
			name:   "custom extension",
			input:  "/tmp/video.mp4",
			suffix: "",
			newExt: ".mp3",
			want:   "/tmp/video.mp3",
		},
		{
			name:   "suffix with new ext",
			input:  "/tmp/video.mp4",
			suffix: "gif",
			newExt: ".gif",
			want:   "/tmp/video_gif.gif",
		},
		{
			name:   "empty suffix keeps name",
			input:  "/tmp/clip.mov",
			suffix: "",
			want:   "/tmp/clip.mov",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OutputPath(tt.input, tt.suffix, tt.newExt)
			if got != tt.want {
				t.Errorf("OutputPath(%q, %q, %q) = %q, want %q",
					tt.input, tt.suffix, tt.newExt, got, tt.want)
			}
		})
	}
}

func TestOutputPathCollision(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "video_muted.mp4")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	input := filepath.Join(dir, "video.mp4")
	got := OutputPath(input, "muted", "")
	want := filepath.Join(dir, "video_muted_1.mp4")
	if got != want {
		t.Errorf("OutputPath with collision = %q, want %q", got, want)
	}
}
