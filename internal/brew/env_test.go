package brew

import (
	"slices"
	"testing"
)

func TestNoninteractiveEnv(t *testing.T) {
	t.Parallel()
	env := noninteractiveEnv()

	if !slices.Contains(env, "NONINTERACTIVE=1") {
		t.Errorf("noninteractiveEnv() missing NONINTERACTIVE=1:\n%v", env)
	}
	// Must extend, not replace, the process environment — otherwise brew loses
	// PATH and friends. os.Environ() always carries at least one entry.
	if len(env) < 2 {
		t.Errorf("noninteractiveEnv() = %d entries, want process env + NONINTERACTIVE", len(env))
	}
}
