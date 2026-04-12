package app

// view_helpers.go — Rendering utility functions: keycap rendering, text
// layout helpers, overlay builders for simple overlays (note preview, help),
// and thin wrappers delegating to [tabstate] sort/filter operations.

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/tui/dbtui/ui"
)

// ── Overlay builders ────────────────────────────────────────────────────────

func (m *Model) buildNotePreviewOverlay() string {
	if m.notePreview == nil {
		return ""
	}
	return m.notePreview.Overlay.View()
}

func (m *Model) buildHelpOverlay() string {
	contentW := ui.OverlayWidth(m.width, helpOverlayMinW, helpOverlayMaxW)

	var b strings.Builder
	b.WriteString(m.overlayHeader("Keyboard Shortcuts"))

	// Render grouped sections using bubbles/help
	navKM := newNavKeyMap()
	visKM := newVisualKeyMap()
	editKM := newEditKeyMap()

	sections := []struct {
		title string
		km    help.KeyMap
	}{
		{"Navigation", navKM},
		{"Visual Mode", visKM},
		{"Edit Mode", editKM},
	}

	for i, sec := range sections {
		b.WriteString(m.styles.HeaderSection().Render(" " + sec.title + " "))
		b.WriteString("\n")
		b.WriteString(m.help.View(sec.km))
		if i < len(sections)-1 {
			b.WriteString("\n\n")
		}
	}

	b.WriteString("\n\n")
	b.WriteString(m.helpItem(keyEsc, "close"))

	return m.styles.OverlayBox().
		Width(contentW).
		Render(b.String())
}

// overlayHeader returns a styled overlay title followed by a blank line.
func (m *Model) overlayHeader(title string) string {
	return m.styles.HeaderSection().Render(" "+title+" ") + "\n\n"
}

// ── Keycap rendering ────────────────────────────────────────────────────────

func (m *Model) helpItem(keys, label string) string {
	keycaps := m.renderKeys(keys)
	desc := m.styles.HeaderHint().Render(label)
	return strings.TrimSpace(fmt.Sprintf("%s %s", keycaps, desc))
}

func (m *Model) helpSeparator() string {
	return m.styles.HeaderHint().Render(" \u00b7 ")
}

func (m *Model) renderKeys(keys string) string {
	if strings.TrimSpace(keys) == "/" {
		return m.keycap("/")
	}
	parts := strings.Split(keys, "/")
	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		rendered = append(rendered, m.keycap(part))
	}
	return joinWithSeparator(m.styles.HeaderHint().Render(" \u00b7 "), rendered...)
}

func (m *Model) keycap(value string) string {
	if len(value) == 1 &&
		((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) {
		return m.styles.Keycap().Render(value)
	}
	return m.styles.Keycap().Render(strings.ToUpper(value))
}

// ── Text layout utilities ───────────────────────────────────────────────────

func filterNonBlank(values []string) []string {
	return lo.Filter(values, func(v string, _ int) bool {
		return strings.TrimSpace(v) != ""
	})
}

func joinVerticalNonEmpty(values ...string) string {
	if f := filterNonBlank(values); len(f) > 0 {
		return lipgloss.JoinVertical(lipgloss.Left, f...)
	}
	return ""
}

func joinWithSeparator(sep string, values ...string) string {
	return strings.Join(filterNonBlank(values), sep)
}

func clampLines(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > maxW {
			lines[i] = ansi.Truncate(line, maxW, "\u2026")
		}
	}
	return strings.Join(lines, "\n")
}

func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	prefix := home + string(os.PathSeparator)
	if rest, ok := strings.CutPrefix(path, prefix); ok {
		return "~" + string(os.PathSeparator) + rest
	}
	return path
}

func truncateLeft(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	sw := lipgloss.Width(s)
	if sw <= maxW {
		return s
	}
	return ansi.TruncateLeft(s, sw-maxW+1, "\u2026")
}

func osc8Link(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}
