package learn

import "github.com/sciminds/cli/internal/uikit"

// newSplitView wires a markdown viewer and cast player into a uikit.SplitView
// with the conventional layout: reader on the left (default-recipient),
// player on the right (specialist).
func newSplitView(title string, viewer *uikit.MdViewer, player *uikit.CastPlayer) *uikit.SplitView {
	return uikit.NewSplitView(title, viewer, player)
}
