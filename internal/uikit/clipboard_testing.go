package uikit

// clipboard_testing.go — small swap helpers used by other packages' tests
// (e.g. internal/tui/dbtui/app) to inject a fake clipboard runner without
// shelling out. Intentionally exported so cross-package tests can call them;
// the names carry "ForTest" so production callers steer clear.

// SetClipboardRunnerForTest replaces the clipboardCmd runner with a fake
// and returns a restore function. The caller is expected to defer the
// restore to keep package state clean across tests.
//
// The dispatch table is set to a single entry named "cat" — a binary
// guaranteed to be on PATH on macOS and Linux — so [Copy]'s LookPath check
// succeeds and the fake runner is invoked. The fake never actually shells
// out: it sees the captured payload and returns whatever error you choose.
func SetClipboardRunnerForTest(run func(name string, args []string, payload string) error) (restore func()) {
	origFn := clipboardCmdFn
	origRun := runClipboardCmd
	clipboardCmdFn = func() []clipboardCmd {
		return []clipboardCmd{{Name: "cat"}}
	}
	runClipboardCmd = func(c clipboardCmd, s string) error {
		return run(c.Name, c.Args, s)
	}
	return func() {
		clipboardCmdFn = origFn
		runClipboardCmd = origRun
	}
}
