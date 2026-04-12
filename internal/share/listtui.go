package share

// listtui.go — interactive TUI for managing cloud shared files. Uses
// bubbles/list with a custom delegate that supports delete, copy-URL,
// and download actions on individual items.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/dustin/go-humanize"
	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/ui"
)

// ErrInterrupted signals the user interrupted the TUI (Ctrl-C).
var ErrInterrupted = errors.New("interrupted")

// cmdTimeout caps how long a single async command (delete, download) waits
// before returning an error.
var cmdTimeout = 30 * time.Second

// ── List item ──────────────────────────────────────────────────────────────

type fileItem struct {
	entry SharedEntry
}

func (i fileItem) Title() string { return i.entry.Name }
func (i fileItem) Description() string {
	sizeType := ui.TUI.Dim().Render(fmt.Sprintf("%s  %s", i.entry.Type, humanize.Bytes(uint64(i.entry.Size))))
	if i.entry.Description != "" {
		return i.entry.Description + "\n" + sizeType
	}
	return sizeType
}
func (i fileItem) FilterValue() string { return i.entry.Name + " " + i.entry.Type }

// ── Delegate key map ───────────────────────────────────────────────────────

type cloudDelegateKeyMap struct {
	remove   key.Binding
	copyURL  key.Binding
	download key.Binding
}

func newCloudDelegateKeyMap() *cloudDelegateKeyMap {
	return &cloudDelegateKeyMap{
		remove: key.NewBinding(
			key.WithKeys("x", "backspace"),
			key.WithHelp("x", "delete"),
		),
		copyURL: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "copy url"),
		),
		download: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "download"),
		),
	}
}

// ── Async result messages ──────────────────────────────────────────────────

type deleteResultMsg struct {
	name string
	err  error
}

type downloadResultMsg struct {
	name string
	path string
	err  error
}

// ── Delegate ───────────────────────────────────────────────────────────────

func newCloudDelegate(keys *cloudDelegateKeyMap, client *cloud.Client, pendingDelete *string) list.DefaultDelegate {
	d := ui.NewListDelegate()

	d.UpdateFunc = func(msg tea.Msg, m *list.Model) tea.Cmd {
		selected, ok := m.SelectedItem().(fileItem)
		if !ok {
			return nil
		}

		keyMsg, ok := msg.(tea.KeyPressMsg)
		if !ok {
			return nil
		}

		switch {
		case key.Matches(keyMsg, keys.remove):
			name := selected.entry.Name
			// Second press on same item → execute delete.
			if *pendingDelete == name {
				*pendingDelete = ""
				m.RemoveItem(m.Index())
				return tea.Batch(
					m.NewStatusMessage(ui.TUI.Warn().Render("Deleting "+name+"…")),
					deleteFile(client, name),
				)
			}
			// First press → arm confirmation.
			*pendingDelete = name
			return m.NewStatusMessage(ui.TUI.Warn().Render("Press x again to delete " + name))

		case key.Matches(keyMsg, keys.copyURL):
			*pendingDelete = ""
			if err := clipboard.WriteAll(selected.entry.URL); err != nil {
				return m.NewStatusMessage(ui.TUI.Fail().Render("Copy failed: " + err.Error()))
			}
			return m.NewStatusMessage(ui.TUI.Pass().Render("Copied URL for " + selected.entry.Name))

		case key.Matches(keyMsg, keys.download):
			*pendingDelete = ""
			name := selected.entry.Name
			return tea.Batch(
				m.NewStatusMessage(ui.TUI.Warn().Render("Downloading "+name+"…")),
				downloadFile(client, name),
			)

		default:
			// Any other key clears pending delete.
			*pendingDelete = ""
		}

		return nil
	}

	help := []key.Binding{keys.remove, keys.copyURL, keys.download}
	d.ShortHelpFunc = func() []key.Binding { return help }
	d.FullHelpFunc = func() [][]key.Binding { return [][]key.Binding{help} }

	return d
}

// ── Async commands ─────────────────────────────────────────────────────────

func deleteFile(client *cloud.Client, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		err := client.Delete(ctx, name)
		return deleteResultMsg{name: name, err: err}
	}
}

func downloadFile(client *cloud.Client, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
		defer cancel()
		outPath := filepath.Base(name)
		f, err := os.Create(outPath)
		if err != nil {
			return downloadResultMsg{name: name, err: err}
		}
		defer func() { _ = f.Close() }()
		if err := client.Download(ctx, name, f); err != nil {
			return downloadResultMsg{name: name, err: err}
		}
		return downloadResultMsg{name: name, path: outPath}
	}
}

// ── Model ──────────────────────────────────────────────────────────────────

type cloudListModel struct {
	list          list.Model
	keys          *cloudDelegateKeyMap
	pendingDelete *string // shared with delegate closure
}

func newCloudListModel(entries []SharedEntry, client *cloud.Client) cloudListModel {
	items := make([]list.Item, len(entries))
	hasDesc := false
	for i, e := range entries {
		items[i] = fileItem{entry: e}
		if e.Description != "" {
			hasDesc = true
		}
	}

	pending := new(string)
	keys := newCloudDelegateKeyMap()
	delegate := newCloudDelegate(keys, client, pending)

	// Use taller items when any entry has a description (title + desc + size).
	if hasDesc {
		delegate.SetHeight(3)
	}

	title := fmt.Sprintf("Cloud Files — %d shared", len(entries))
	l := list.New(items, delegate, 0, 0)
	l.Title = title
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}

	return cloudListModel{list: l, keys: keys, pendingDelete: pending}
}

func (m cloudListModel) Init() tea.Cmd { return nil }

func (m cloudListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	case deleteResultMsg:
		if msg.err != nil {
			return m, m.list.NewStatusMessage(ui.TUI.Fail().Render("Delete failed: " + msg.err.Error()))
		}
		return m, m.list.NewStatusMessage(ui.TUI.Pass().Render("Deleted " + msg.name))
	case downloadResultMsg:
		if msg.err != nil {
			return m, m.list.NewStatusMessage(ui.TUI.Fail().Render("Download failed: " + msg.err.Error()))
		}
		return m, m.list.NewStatusMessage(ui.TUI.Pass().Render("Downloaded " + msg.path))
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m cloudListModel) View() tea.View {
	v := tea.NewView(m.list.View())
	v.AltScreen = true
	return v
}

// RunCloudListTUI launches the interactive cloud file manager.
func RunCloudListTUI(entries []SharedEntry, client *cloud.Client) error {
	m := newCloudListModel(entries, client)
	p := tea.NewProgram(m)
	_, err := p.Run()
	ui.DrainStdin()
	if err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}
