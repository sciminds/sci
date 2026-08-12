package cli

import (
	"testing"

	"github.com/sciminds/sci/internal/zot"
)

func TestWatchStdinEOF(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		runnerEnv string
		stdinTTY  bool
		want      bool
	}{
		// The only armed case: the delegated remote end of runner=ssh
		// (env marker set by BuildRemoteArgs) with no pty — the one
		// configuration where a vanishing ssh client delivers no signal,
		// only a closed stdin pipe.
		{"delegated remote, no tty", zot.RunnerLocal, false, true},
		{"delegated remote, tty (ssh -t)", zot.RunnerLocal, true, false},
		{"plain local run, piped stdin", "", false, false},
		{"plain local run, tty", "", true, false},
		{"runner=ssh env (delegating side)", zot.RunnerSSH, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := watchStdinEOF(tt.runnerEnv, tt.stdinTTY); got != tt.want {
				t.Errorf("watchStdinEOF(%q, %v) = %v, want %v", tt.runnerEnv, tt.stdinTTY, got, tt.want)
			}
		})
	}
}
