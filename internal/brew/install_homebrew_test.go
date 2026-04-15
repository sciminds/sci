package brew

import (
	"errors"
	"os/exec"
	"testing"
)

// InstallHomebrew should no-op (return ErrHomebrewInstalled) when brew is
// already on PATH, so running it on a developer machine or CI runner with
// preinstalled brew doesn't re-trigger the installer.
func TestInstallHomebrew_AlreadyInstalled(t *testing.T) {
	if _, err := exec.LookPath("brew"); err != nil {
		t.Skip("brew not on PATH; this test only runs on machines with brew installed")
	}
	err := InstallHomebrew()
	if !errors.Is(err, ErrHomebrewInstalled) {
		t.Errorf("InstallHomebrew with brew present should return ErrHomebrewInstalled, got: %v", err)
	}
}
