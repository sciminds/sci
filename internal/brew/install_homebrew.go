package brew

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// ErrHomebrewInstalled indicates Homebrew is already on PATH; no work to do.
var ErrHomebrewInstalled = errors.New("homebrew is already installed")

// InstallHomebrew runs the official Homebrew installer script non-interactively.
// Output is streamed to stderr so the user sees progress (the installer can
// take several minutes). Returns ErrHomebrewInstalled if brew is already
// on PATH — callers should treat that as a no-op success.
//
// After a successful install, the brew binary lands at /opt/homebrew/bin/brew
// (Apple Silicon) or /usr/local/bin/brew (Intel). It may not yet be on PATH
// in the current process — callers should print the shellenv hint.
func InstallHomebrew() error {
	if _, err := exec.LookPath("brew"); err == nil {
		return ErrHomebrewInstalled
	}

	const url = "https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh"
	script := fmt.Sprintf(`/bin/bash -c "$(curl -fsSL %s)"`, url)
	cmd := exec.Command("/bin/bash", "-c", script)
	cmd.Env = append(os.Environ(), "NONINTERACTIVE=1")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
