package uikit

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestOverlayInnerWidth_TracksFrame is the regression guard for overlay width:
// the inner width must be derived from the style's horizontal frame, so changing
// the overlay border/padding shifts the inner width by exactly the frame delta
// instead of silently drifting from a hardcoded constant.
func TestOverlayInnerWidth_TracksFrame(t *testing.T) {
	t.Parallel()

	base := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	// Horizontal frame = border(2) + padding(2+2) = 6.
	if got, want := OverlayInnerWidth(50, base), 44; got != want {
		t.Fatalf("OverlayInnerWidth(50, base) = %d, want %d", got, want)
	}

	// Grow horizontal padding by 2 per side (+4 frame). Inner width must shrink
	// by exactly 4 — proving it tracks the style, not a constant.
	wider := base.Padding(1, 4)
	if got, want := OverlayInnerWidth(50, wider), 40; got != want {
		t.Fatalf("OverlayInnerWidth(50, widerPadding) = %d, want %d", got, want)
	}

	// Dropping the border (−2 frame) must widen the inner area by exactly 2.
	noBorder := base.Border(lipgloss.HiddenBorder(), false)
	if got, want := OverlayInnerWidth(50, noBorder), 46; got != want {
		t.Fatalf("OverlayInnerWidth(50, noBorder) = %d, want %d", got, want)
	}

	// Frame wider than the content clamps to >= 1.
	if got := OverlayInnerWidth(2, base); got < 1 {
		t.Fatalf("OverlayInnerWidth(2, base) = %d, want >= 1", got)
	}
}

// TestOverlayBodyBudget_TracksFrame is the regression guard for overlay height:
// the body budget must fall by exactly the vertical-frame delta when border or
// padding grows, and by one line for each chrome line added — never a stale
// hardcoded chrome count.
func TestOverlayBodyBudget_TracksFrame(t *testing.T) {
	t.Parallel()

	base := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	const termH, contentW = 30, 40
	prefix := "Title\n\nfilter\n\n"
	suffix := "\n\nstatus\n\nhints"

	budget := OverlayBodyBudget(termH, contentW, base, prefix, suffix)

	// Growing vertical padding by 1 per side (+2 frame) must drop the budget by 2.
	taller := base.Padding(2, 2)
	if got, want := OverlayBodyBudget(termH, contentW, taller, prefix, suffix), budget-2; got != want {
		t.Fatalf("body budget after +2 vertical frame = %d, want %d (was %d)", got, want, budget)
	}

	// Adding a single chrome line to the prefix must drop the budget by exactly 1.
	if got, want := OverlayBodyBudget(termH, contentW, base, prefix+"extra\n", suffix), budget-1; got != want {
		t.Fatalf("body budget after +1 prefix line = %d, want %d (was %d)", got, want, budget)
	}

	// Floor: a tiny terminal clamps to OverlayMinH, never negative.
	if got := OverlayBodyBudget(2, contentW, base, prefix, suffix); got != OverlayMinH {
		t.Fatalf("body budget for tiny terminal = %d, want OverlayMinH (%d)", got, OverlayMinH)
	}
}

// TestOverlayBodyBudget_AccountsForWrap proves the budget shrinks when a chrome
// line is too wide for the box and wraps — the exact bug a raw line-count probe
// would miss (it would count the long hint as one line, then overflow when the
// frame wraps it). A suffix that wraps to two lines must cost one more body line
// than the same suffix that fits on one.
func TestOverlayBodyBudget_AccountsForWrap(t *testing.T) {
	t.Parallel()

	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	const termH, contentW = 30, 20 // inner width = 20 - 6 = 14
	prefix := "Title\n\n"

	short := OverlayBodyBudget(termH, contentW, style, prefix, "\n\nesc")
	long := OverlayBodyBudget(termH, contentW, style, prefix, "\n\nthis hint is far too wide to fit on one line")
	// A raw line-count probe would treat the long hint as one line and return the
	// same budget; rendering through the frame wraps it, so the budget must drop.
	if long >= short {
		t.Fatalf("wrapping suffix budget = %d, want < non-wrapping %d (wrap not accounted for)", long, short)
	}
}

// TestOverlayBodyBudget_FitsTerminal proves the central invariant the measured
// approach buys us: an overlay assembled as prefix + body + suffix and rendered
// through the frame style fills the terminal height exactly when the budget is
// not clamped to the OverlayMinH floor — never overflowing, never wasting space —
// regardless of how many chrome lines the prefix/suffix carry. (Below the floor,
// i.e. a terminal too small for chrome + minimum body, the overlay is allowed to
// exceed termH; that is the floor doing its job.)
func TestOverlayBodyBudget_FitsTerminal(t *testing.T) {
	t.Parallel()

	style := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	const contentW = 40
	for _, termH := range []int{20, 30, 50} {
		prefix := "Header\n\nlabel\n\n"
		suffix := "\n\nstatus\n\nhint line"
		budget := OverlayBodyBudget(termH, contentW, style, prefix, suffix)
		if budget <= OverlayMinH {
			t.Fatalf("termH=%d: budget %d unexpectedly at floor", termH, budget)
		}

		body := strings.TrimRight(strings.Repeat("row\n", budget), "\n")
		overlay := style.Width(contentW).Render(prefix + body + suffix)

		if h := lipgloss.Height(overlay); h != termH {
			t.Errorf("termH=%d: overlay height %d, want exact fit", termH, h)
		}
	}
}
