// layout_flex.go — builder-style layout composition inspired by CSS Flexbox.
//
// VStack and HStack provide a declarative way to compose vertical and
// horizontal layouts with Fixed (measured) and Flex (proportional) children,
// plus Gap spacing. They replace manual lipgloss.JoinVertical/JoinHorizontal
// calls and ad-hoc height/width arithmetic.
//
//	// Classic TUI chrome: title + body + status bar
//	uikit.VStack(width, height).
//	    Fixed(renderTitle).       // measured, takes natural height
//	    Flex(1, renderBody).      // takes remaining space
//	    Fixed(renderStatus).      // measured, takes natural height
//	    Render()
//
//	// Side-by-side panels: 30% sidebar + 70% main
//	uikit.HStack(width, height).
//	    Flex(0.3, renderSidebar).
//	    Gap(1).
//	    Flex(0.7, renderMain).
//	    Render()

package uikit

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// direction distinguishes vertical from horizontal stacks.
type direction int

const (
	vertical direction = iota
	horizontal
)

// childKind distinguishes the three types of stack children.
type childKind int

const (
	kindFixed childKind = iota
	kindFlex
	kindGap
)

// stackChild is one entry in a Stack's child list.
type stackChild struct {
	kind childKind

	// Fixed callbacks.
	// VStack Fixed: func(width int) string
	// HStack Fixed: func(height int) string
	fixedFn func(int) string

	// Flex callbacks.
	// Both VStack and HStack: func(width, height int) string
	flexFn    func(int, int) string
	flexRatio float64

	// Gap size (lines for VStack, columns for HStack).
	gapSize int
}

// Stack is a builder for composing vertical or horizontal layouts.
// Use [VStack] or [HStack] to create one.
type Stack struct {
	dir      direction
	width    int
	height   int
	children []stackChild
}

// VStack creates a vertical stack builder. Children are composed top-to-bottom.
// The final output is always exactly height lines tall.
//
//   - [Stack.Fixed]: measured content, takes its natural height.
//   - [Stack.Flex]: proportional content, shares remaining height by ratio.
//   - [Stack.Gap]: blank lines between children.
func VStack(width, height int) *Stack {
	return &Stack{dir: vertical, width: width, height: height}
}

// HStack creates a horizontal stack builder. Children are composed left-to-right.
//
//   - [Stack.Fixed]: measured content, takes its natural width.
//   - [Stack.Flex]: proportional content, shares remaining width by ratio.
//   - [Stack.Gap]: space columns between children.
func HStack(width, height int) *Stack {
	return &Stack{dir: horizontal, width: width, height: height}
}

// Fixed adds a child whose size is measured from its rendered content.
//
// For VStack: fn receives the available width and returns rendered content.
// The content's height is measured and subtracted from the remaining budget.
//
// For HStack: fn receives the available height and returns rendered content.
// The content's width is measured and subtracted from the remaining budget.
func (s *Stack) Fixed(fn func(int) string) *Stack {
	s.children = append(s.children, stackChild{kind: kindFixed, fixedFn: fn})
	return s
}

// Flex adds a child that receives a proportional share of remaining space.
//
// The ratio is relative to other Flex children: two children with ratios 1
// and 3 get 25% and 75% respectively. Fractional ratios like 0.3 and 0.7
// also work.
//
// The callback receives (width, height) for VStack, or (width, height) for
// HStack — the allocated dimensions for this child.
func (s *Stack) Flex(ratio float64, fn func(width, height int) string) *Stack {
	if ratio <= 0 {
		ratio = 1
	}
	s.children = append(s.children, stackChild{kind: kindFlex, flexFn: fn, flexRatio: ratio})
	return s
}

// Gap adds spacing between children.
//
// For VStack: n blank lines. For HStack: n space columns.
func (s *Stack) Gap(n int) *Stack {
	if n < 0 {
		n = 0
	}
	s.children = append(s.children, stackChild{kind: kindGap, gapSize: n})
	return s
}

// Render computes the layout and returns the composed string.
func (s *Stack) Render() string {
	if s.width <= 0 || s.height <= 0 {
		return ""
	}
	if s.dir == vertical {
		return s.renderVertical()
	}
	return s.renderHorizontal()
}

// ── Vertical rendering ────────────────────────────────────────────────────

func (s *Stack) renderVertical() string {
	// Phase 1: render Fixed children and measure their heights.
	// Track remaining height budget for Flex children.
	remaining := s.height
	totalRatio := 0.0

	type rendered struct {
		content string
		height  int
	}

	fixedResults := make(map[int]rendered, len(s.children))

	for i, child := range s.children {
		switch child.kind {
		case kindFixed:
			content := child.fixedFn(s.width)
			h := lipgloss.Height(content)
			fixedResults[i] = rendered{content: content, height: h}
			remaining -= h
		case kindGap:
			remaining -= child.gapSize
		case kindFlex:
			totalRatio += child.flexRatio
		}
	}

	if remaining < 0 {
		remaining = 0
	}

	// Phase 2: render Flex children with allocated heights.
	flexResults := make(map[int]rendered, len(s.children))
	flexHeightsUsed := 0

	flexChildren := make([]int, 0, len(s.children))
	for i, child := range s.children {
		if child.kind == kindFlex {
			flexChildren = append(flexChildren, i)
		}
	}

	for j, idx := range flexChildren {
		child := s.children[idx]
		var h int
		if j == len(flexChildren)-1 {
			// Last flex child gets whatever is left (avoids rounding errors).
			h = remaining - flexHeightsUsed
		} else {
			h = int(float64(remaining) * child.flexRatio / totalRatio)
		}
		if h < 1 {
			h = 1
		}
		content := child.flexFn(s.width, h)
		content = FitHeight(content, h)
		flexResults[idx] = rendered{content: content, height: h}
		flexHeightsUsed += h
	}

	// Phase 3: assemble all sections in order.
	parts := make([]string, 0, len(s.children))
	usedHeight := 0

	for i, child := range s.children {
		switch child.kind {
		case kindFixed:
			r := fixedResults[i]
			parts = append(parts, r.content)
			usedHeight += r.height
		case kindFlex:
			r := flexResults[i]
			parts = append(parts, r.content)
			usedHeight += r.height
		case kindGap:
			if child.gapSize > 0 {
				parts = append(parts, strings.Repeat("\n", child.gapSize-1))
				usedHeight += child.gapSize
			}
		}
	}

	result := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Pad or truncate to exact height.
	return FitHeight(result, s.height)
}

// ── Horizontal rendering ──────────────────────────────────────────────────

func (s *Stack) renderHorizontal() string {
	// Phase 1: render Fixed children and measure their widths.
	remaining := s.width
	totalRatio := 0.0

	type rendered struct {
		content string
		width   int
	}

	fixedResults := make(map[int]rendered, len(s.children))

	for i, child := range s.children {
		switch child.kind {
		case kindFixed:
			content := child.fixedFn(s.height)
			w := lipgloss.Width(content)
			fixedResults[i] = rendered{content: content, width: w}
			remaining -= w
		case kindGap:
			remaining -= child.gapSize
		case kindFlex:
			totalRatio += child.flexRatio
		}
	}

	if remaining < 0 {
		remaining = 0
	}

	// Phase 2: render Flex children with allocated widths.
	flexResults := make(map[int]rendered, len(s.children))
	flexWidthsUsed := 0

	flexChildren := make([]int, 0, len(s.children))
	for i, child := range s.children {
		if child.kind == kindFlex {
			flexChildren = append(flexChildren, i)
		}
	}

	for j, idx := range flexChildren {
		child := s.children[idx]
		var w int
		if j == len(flexChildren)-1 {
			w = remaining - flexWidthsUsed
		} else {
			w = int(float64(remaining) * child.flexRatio / totalRatio)
		}
		if w < 1 {
			w = 1
		}
		content := child.flexFn(w, s.height)
		// Ensure content fills the allocated width by applying a style.
		content = lipgloss.NewStyle().Width(w).Height(s.height).Render(content)
		flexResults[idx] = rendered{content: content, width: w}
		flexWidthsUsed += w
	}

	// Phase 3: assemble all sections left-to-right.
	parts := make([]string, 0, len(s.children))

	for i, child := range s.children {
		switch child.kind {
		case kindFixed:
			r := fixedResults[i]
			// Ensure fixed content fills the stack height.
			content := lipgloss.NewStyle().Height(s.height).Render(r.content)
			parts = append(parts, content)
		case kindFlex:
			r := flexResults[i]
			parts = append(parts, r.content)
		case kindGap:
			if child.gapSize > 0 {
				spacer := lipgloss.NewStyle().
					Width(child.gapSize).
					Height(s.height).
					Render("")
				parts = append(parts, spacer)
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}
