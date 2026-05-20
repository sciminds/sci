//go:build linux

package uikit

func init() {
	clipboardCmdFn = func() []clipboardCmd {
		return []clipboardCmd{
			{Name: "wl-copy"},
			{Name: "xclip", Args: []string{"-selection", "clipboard"}},
			{Name: "xsel", Args: []string{"--clipboard", "--input"}},
		}
	}
}
