package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/dustin/go-humanize"
	"github.com/sciminds/cli/internal/uikit"
)

// View implements tea.Model. Dispatch by screen.
func (m *Model) View() tea.View {
	var body string
	switch m.screen {
	case screenBrowse:
		body = m.viewBrowse()
	case screenConfirm:
		body = m.viewConfirm()
	case screenTransfer:
		body = m.viewTransfer()
	case screenError:
		body = m.viewError()
	case screenDone:
		body = m.viewDone()
	}
	v := tea.NewView(body)
	v.AltScreen = true
	return v
}

func (m *Model) viewBrowse() string {
	var b strings.Builder
	header := uikit.TUI.TextBlue().Render(m.Breadcrumb())
	if n := m.SelectedCount(); n > 0 {
		header += uikit.TUI.Dim().Render(fmt.Sprintf("   [%d selected]", n))
	}
	b.WriteString(header)
	b.WriteString("\n\n")

	switch {
	case m.loadErr != nil:
		b.WriteString(uikit.TUI.Fail().Render("error: " + m.loadErr.Error()))
		b.WriteByte('\n')
	case m.loading:
		b.WriteString(uikit.TUI.Dim().Render("loading…"))
		b.WriteByte('\n')
	case len(m.entries) == 0:
		b.WriteString(uikit.TUI.Dim().Render("(empty directory)"))
		b.WriteByte('\n')
	default:
		for i, e := range m.entries {
			cursor := "  "
			if i == m.cursor {
				cursor = "▌ "
			}
			mark := "[ ]"
			if m.isSelected(m.pathFor(e)) {
				mark = uikit.TUI.Pass().Render("[x]")
			}
			name := e.Name
			if e.IsDir {
				name = uikit.TUI.TextBlue().Render(name + "/")
			} else if e.IsLink {
				name = uikit.TUI.Dim().Render(name + "@")
			}
			fmt.Fprintf(&b, "%s%s %s\n", cursor, mark, name)
		}
	}

	b.WriteByte('\n')
	b.WriteString(uikit.TUI.Dim().Render("space select · enter descend · backspace up · d download · q quit"))
	return b.String()
}

func (m *Model) viewConfirm() string {
	var b strings.Builder
	b.WriteString(uikit.TUI.TextBlue().Render("Confirm download"))
	b.WriteString("\n\n")
	for _, p := range m.SelectedPaths() {
		fmt.Fprintf(&b, "  %s\n", p)
	}
	b.WriteByte('\n')
	switch {
	case m.sizeErr != nil:
		b.WriteString(uikit.TUI.Fail().Render("size probe failed: " + m.sizeErr.Error()))
		b.WriteByte('\n')
	case m.sizeProbing:
		b.WriteString(uikit.TUI.Dim().Render("measuring total size…"))
		b.WriteByte('\n')
	default:
		fmt.Fprintf(&b, "  Total: %s (%d bytes) across %d item(s)\n",
			humanize.Bytes(uint64(m.totalBytes)), m.totalBytes, len(m.selected))
	}
	b.WriteByte('\n')
	b.WriteString(uikit.TUI.Dim().Render("y download · n cancel"))
	return b.String()
}

func (m *Model) viewTransfer() string {
	var b strings.Builder
	b.WriteString(uikit.TUI.TextBlue().Render("Downloading"))
	fmt.Fprintf(&b, "   [%d/%d]\n\n", m.queueIdx+1, len(m.queue))
	if m.queueIdx < len(m.queue) {
		fmt.Fprintf(&b, "  %s\n\n", m.queue[m.queueIdx])
	}
	b.WriteString("  ")
	b.WriteString(m.progressBar.View())
	b.WriteByte('\n')
	fmt.Fprintf(&b, "  %s · %s · ETA %s\n",
		humanize.Bytes(uint64(m.progress.Bytes)),
		nonEmpty(m.progress.Rate, "—"),
		nonEmpty(m.progress.ETA, "—"),
	)
	b.WriteByte('\n')
	b.WriteString(uikit.TUI.Dim().Render("ctrl-c cancel (resumable on next run)"))
	return b.String()
}

func (m *Model) viewError() string {
	var b strings.Builder
	b.WriteString(uikit.TUI.Fail().Render("Transfer failed"))
	b.WriteString("\n\n")
	if m.queueIdx < len(m.queue) {
		fmt.Fprintf(&b, "  %s\n", m.queue[m.queueIdx])
	}
	if m.transferErr != nil {
		for _, line := range strings.Split(m.transferErr.Error(), "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	b.WriteByte('\n')
	b.WriteString(uikit.TUI.Dim().Render("r retry · s skip · q abort"))
	return b.String()
}

func (m *Model) viewDone() string {
	var b strings.Builder
	b.WriteString(uikit.TUI.Pass().Render("Done"))
	fmt.Fprintf(&b, "   downloaded %d/%d item(s)\n\n", m.transferred, len(m.queue))
	b.WriteString(uikit.TUI.Dim().Render("enter to exit"))
	return b.String()
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
