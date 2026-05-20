package uikit

// clipboard.go — cross-platform "copy to system clipboard" helper.
// Platform dispatch tables live in clipboard_darwin.go / clipboard_linux.go.

import (
	"fmt"
	"os/exec"
	"strings"
)

// clipboardCmd is one candidate clipboard tool. Args carry any flags the
// command needs (e.g. "xclip -selection clipboard").
type clipboardCmd struct {
	Name string
	Args []string
}

// clipboardCmdFn returns the ordered list of clipboard tools to try.
// Overridden by clipboard_{darwin,linux}.go via init(); tests stub it.
var clipboardCmdFn = func() []clipboardCmd { return nil }

// runClipboardCmd is the indirection point for tests — it shells out to the
// named tool, piping s as stdin. Tests overwrite it with a fake.
var runClipboardCmd = func(c clipboardCmd, s string) error {
	cmd := exec.Command(c.Name, c.Args...)
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

// Copy writes s to the system clipboard. Tries each platform-specific tool
// in order, returning the first success. If none are available it returns an
// error naming every tool it tried so the user knows which one to install.
func Copy(s string) error {
	candidates := clipboardCmdFn()
	if len(candidates) == 0 {
		return fmt.Errorf("no clipboard tool available on this platform")
	}

	var tried []string
	for _, c := range candidates {
		if _, err := exec.LookPath(c.Name); err != nil {
			tried = append(tried, c.Name)
			continue
		}
		if err := runClipboardCmd(c, s); err != nil {
			tried = append(tried, c.Name)
			continue
		}
		return nil
	}
	return fmt.Errorf("no clipboard tool found (tried: %s)", strings.Join(tried, ", "))
}
