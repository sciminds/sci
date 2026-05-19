package uikit

import (
	"os"
	"testing"
)

// TestMain triggers DetectTermStyle's sync.Once once before any parallel test
// goroutine spawns, so the write to styleName happens-before every concurrent
// test that reads it through renderLocked. Without this, the Once.Do call
// from TestDetectTermStyle races against parallel tests that go through
// RenderMarkdown.
func TestMain(m *testing.M) {
	DetectTermStyle()
	os.Exit(m.Run())
}
