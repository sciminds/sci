package cmdutil

import (
	"os"
	"testing"
)

// TestMain triggers SetupHelp's sync.Once once before any parallel test
// goroutine spawns, so the write to cli.HelpPrinterCustom happens-before
// every concurrent test that reads it through cli.Command.Run(). Without
// this, the Once.Do call from one parallel test races against another
// test's read of the same global.
func TestMain(m *testing.M) {
	SetupHelp(nil)
	os.Exit(m.Run())
}
