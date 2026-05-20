//go:build darwin

package uikit

func init() {
	clipboardCmdFn = func() []clipboardCmd {
		return []clipboardCmd{{Name: "pbcopy"}}
	}
}
