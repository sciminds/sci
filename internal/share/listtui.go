package share

// listtui.go — interactive hierarchical browser for the cloud bucket.
// Renders the folder/file children at a current path; Enter descends into
// folders, Backspace ascends, and per-file actions (delete, copy-url,
// download) reuse the same bubbles/v2 list delegate pattern as before.
//
// Two important rules baked into the delegate:
//
//   - Folders cannot be deleted/downloaded/URL-copied from here — those
//     actions only apply to the leaf file. Trying them surfaces a friendly
//     toast so users learn the rule without panicking.
//   - Delete is restricted to files whose key starts with the current
//     user's prefix. Pressing `x` on someone else's file produces a status
//     message rather than an HF auth failure mid-network.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/dustin/go-humanize"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cloud"
	"github.com/sciminds/cli/internal/uikit"
)

// ErrInterrupted signals the user interrupted the TUI (Ctrl-C).
var ErrInterrupted = errors.New("interrupted")

// cmdTimeout caps how long a single async command (delete, download)
// waits before returning an error.
var cmdTimeout = 30 * time.Second

// ── List item ──────────────────────────────────────────────────────────────

type entryItem struct {
	entry TreeEntry
}

// Title implements list.DefaultItem. Folders get a trailing slash so they
// read as folders without depending on the description column for context.
func (i entryItem) Title() string {
	if i.entry.IsDir {
		return i.entry.Name + "/"
	}
	return i.entry.Name
}

// Description implements list.DefaultItem.
func (i entryItem) Description() string {
	if i.entry.IsDir {
		return uikit.TUI.Dim().Render("folder")
	}
	return uikit.TUI.Dim().Render(fmt.Sprintf("%s  %s",
		detectFileType(i.entry.Name),
		humanize.Bytes(uint64(i.entry.Size)),
	))
}

// FilterValue implements list.Item.
func (i entryItem) FilterValue() string { return i.entry.Name }

// ── Delegate key map ───────────────────────────────────────────────────────

type browseKeyMap struct {
	open     key.Binding
	up       key.Binding
	remove   key.Binding
	copyURL  key.Binding
	download key.Binding
}

func newBrowseKeyMap() *browseKeyMap {
	return &browseKeyMap{
		open: key.NewBinding(
			key.WithKeys("enter", "right", "l"),
			key.WithHelp("enter", "open"),
		),
		up: key.NewBinding(
			key.WithKeys("backspace", "left", "h"),
			key.WithHelp("⌫", "up"),
		),
		remove: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "delete (own files)"),
		),
		copyURL: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "copy url"),
		),
		download: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "download (folder = sync)"),
		),
	}
}

// ── Async result types ────────────────────────────────────────────────────
// Used with uikit.AsyncCmdCtx → uikit.Result[T] for type-safe dispatch.

type deleteOK struct {
	key  string // full bucket key, including owner prefix
	name string // basename, for display
}
type downloadOK struct {
	key  string
	path string
}

// ── Delegate ───────────────────────────────────────────────────────────────

func newBrowseDelegate(keys *browseKeyMap, client *cloud.Client, pendingDelete *string) list.DefaultDelegate {
	d := uikit.NewListDelegate()

	d.UpdateFunc = func(msg tea.Msg, m *list.Model) tea.Cmd {
		item, ok := m.SelectedItem().(entryItem)
		if !ok {
			return nil
		}
		keyMsg, ok := msg.(tea.KeyPressMsg)
		if !ok {
			return nil
		}

		// open/up are handled in the model so they trigger navigation
		// (cwd change + rebuild). Per-file actions belong here.
		switch {
		case key.Matches(keyMsg, keys.remove):
			return handleRemove(m, item, client, pendingDelete)
		case key.Matches(keyMsg, keys.copyURL):
			*pendingDelete = ""
			return handleCopyURL(m, item)
		case key.Matches(keyMsg, keys.download):
			*pendingDelete = ""
			return handleDownload(m, item, client)
		default:
			// Any other key clears a pending-delete confirmation.
			*pendingDelete = ""
		}
		return nil
	}

	help := []key.Binding{keys.open, keys.up, keys.download, keys.remove, keys.copyURL}
	d.ShortHelpFunc = func() []key.Binding { return help }
	d.FullHelpFunc = func() [][]key.Binding { return [][]key.Binding{help} }

	return d
}

// handleRemove enforces ownership + folder rules before the two-press
// confirm dance. Surfaces a status toast for every rejected case so the
// user learns the rule without leaving the TUI.
func handleRemove(m *list.Model, item entryItem, client *cloud.Client, pendingDelete *string) tea.Cmd {
	if item.entry.IsDir {
		*pendingDelete = ""
		return m.NewStatusMessage(uikit.TUI.Warn().Render(
			"cannot delete folders — descend to remove individual files",
		))
	}
	if owner := item.entry.Owner(); owner != client.Username {
		*pendingDelete = ""
		return m.NewStatusMessage(uikit.TUI.Warn().Render(
			fmt.Sprintf("cannot delete @%s's files — only the owner can", owner),
		))
	}
	// Second press on the same item → execute the delete.
	if *pendingDelete == item.entry.Key {
		*pendingDelete = ""
		m.RemoveItem(m.Index())
		return tea.Batch(
			m.NewStatusMessage(uikit.TUI.Warn().Render("Deleting "+item.entry.Name+"…")),
			deleteFile(client, item.entry.Key, item.entry.Name),
		)
	}
	*pendingDelete = item.entry.Key
	return m.NewStatusMessage(uikit.TUI.Warn().Render(
		"Press x again to delete " + item.entry.Name,
	))
}

func handleCopyURL(m *list.Model, item entryItem) tea.Cmd {
	if item.entry.IsDir {
		return m.NewStatusMessage(uikit.TUI.Warn().Render("no URL for folders"))
	}
	if item.entry.URL == "" {
		return m.NewStatusMessage(uikit.TUI.Warn().Render(
			"private bucket has no public URL — use d to download",
		))
	}
	if err := clipboard.WriteAll(item.entry.URL); err != nil {
		return m.NewStatusMessage(uikit.TUI.Fail().Render("Copy failed: " + err.Error()))
	}
	return m.NewStatusMessage(uikit.TUI.Pass().Render("Copied URL for " + item.entry.Name))
}

func handleDownload(m *list.Model, item entryItem, client *cloud.Client) tea.Cmd {
	if item.entry.IsDir {
		return tea.Batch(
			m.NewStatusMessage(uikit.TUI.Warn().Render("Syncing "+item.entry.Name+"/…")),
			downloadFolder(client, item.entry.Key, item.entry.Name),
		)
	}
	return tea.Batch(
		m.NewStatusMessage(uikit.TUI.Warn().Render("Downloading "+item.entry.Name+"…")),
		downloadFile(client, item.entry.Key, item.entry.Name),
	)
}

// ── Async commands ─────────────────────────────────────────────────────────

func deleteFile(client *cloud.Client, fullKey, name string) tea.Cmd {
	return uikit.AsyncCmdCtx(context.Background(), cmdTimeout, func(ctx context.Context) (deleteOK, error) {
		// Delete takes the filename within the client's own user prefix.
		// We already gated by Owner==Username, so stripping is safe.
		filename := strings.TrimPrefix(fullKey, client.Username+"/")
		return deleteOK{key: fullKey, name: name}, client.Delete(ctx, filename)
	})
}

func downloadFile(client *cloud.Client, fullKey, name string) tea.Cmd {
	return uikit.AsyncCmdCtx(context.Background(), transferTimeout, func(ctx context.Context) (downloadOK, error) {
		outPath := filepath.Base(name)
		f, err := os.Create(outPath)
		if err != nil {
			return downloadOK{key: fullKey}, err
		}
		defer func() { _ = f.Close() }()
		if err := client.DownloadByKey(ctx, fullKey, f); err != nil {
			return downloadOK{key: fullKey, path: outPath}, err
		}
		return downloadOK{key: fullKey, path: outPath}, nil
	})
}

// downloadFolder syncs every object under fullKey into a same-named
// local directory in cwd. Mirrors `sci cloud get <folder>` behavior.
func downloadFolder(client *cloud.Client, fullKey, name string) tea.Cmd {
	return uikit.AsyncCmdCtx(context.Background(), folderSyncTimeout, func(ctx context.Context) (downloadOK, error) {
		outDir := filepath.Base(name)
		if err := client.Sync(ctx, fullKey, outDir); err != nil {
			return downloadOK{key: fullKey, path: outDir}, err
		}
		return downloadOK{key: fullKey, path: outDir + "/"}, nil
	})
}

// ── Model ──────────────────────────────────────────────────────────────────

type cloudBrowseModel struct {
	list          list.Model
	keys          *browseKeyMap
	client        *cloud.Client
	objects       []cloud.ObjectInfo // full bucket listing, mutated on delete
	cwd           string             // bucket-relative path; "" = root
	pendingDelete *string            // shared with delegate closure
}

func newCloudBrowseModel(objects []cloud.ObjectInfo, client *cloud.Client) cloudBrowseModel {
	pending := new(string)
	keys := newBrowseKeyMap()
	delegate := newBrowseDelegate(keys, client, pending)

	l := list.New(nil, delegate, 0, 0)
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit"))}
	}

	m := cloudBrowseModel{
		list:          l,
		keys:          keys,
		client:        client,
		objects:       objects,
		cwd:           "",
		pendingDelete: pending,
	}
	m.rebuild()
	return m
}

// rebuild repopulates the list with the immediate children at cwd. Called
// after every navigation (cd in/out) and after deletes that mutate
// m.objects.
func (m *cloudBrowseModel) rebuild() {
	entries := ChildrenAt(m.objects, m.cwd)
	items := lo.Map(entries, func(e TreeEntry, _ int) list.Item {
		return entryItem{entry: e}
	})
	m.list.SetItems(items)
	m.list.Title = m.breadcrumb()
}

// breadcrumb renders cwd as `sciminds/private / ejolly / data` for the
// list title.
func (m cloudBrowseModel) breadcrumb() string {
	base := "cloud"
	if m.client != nil {
		base = "sciminds/" + m.client.Bucket
	}
	if m.cwd == "" {
		return base
	}
	return base + " / " + strings.ReplaceAll(m.cwd, "/", " / ")
}

// Init implements tea.Model.
func (m cloudBrowseModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m cloudBrowseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch {
		case key.Matches(msg, m.keys.open):
			if item, ok := m.list.SelectedItem().(entryItem); ok && item.entry.IsDir {
				m.cwd = item.entry.Key
				*m.pendingDelete = ""
				m.rebuild()
				m.list.ResetSelected()
				return m, nil
			}
			// Files: Enter is intentionally inert — `d` downloads.
		case key.Matches(msg, m.keys.up):
			if m.cwd != "" {
				m.cwd = ParentPath(m.cwd)
				*m.pendingDelete = ""
				m.rebuild()
				m.list.ResetSelected()
				return m, nil
			}
		}
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	case uikit.Result[deleteOK]:
		if msg.Err != nil {
			return m, m.list.NewStatusMessage(uikit.TUI.Fail().Render("Delete failed: " + msg.Err.Error()))
		}
		// Drop the deleted key so any later ChildrenAt() rebuild matches reality.
		m.objects = lo.Filter(m.objects, func(o cloud.ObjectInfo, _ int) bool {
			return o.Key != msg.Value.key
		})
		return m, m.list.NewStatusMessage(uikit.TUI.Pass().Render("Deleted " + msg.Value.name))
	case uikit.Result[downloadOK]:
		if msg.Err != nil {
			return m, m.list.NewStatusMessage(uikit.TUI.Fail().Render("Download failed: " + msg.Err.Error()))
		}
		return m, m.list.NewStatusMessage(uikit.TUI.Pass().Render("Downloaded " + msg.Value.path))
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m cloudBrowseModel) View() tea.View {
	v := tea.NewView(m.list.View())
	v.AltScreen = true
	return v
}

// RunCloudBrowseTUI launches the hierarchical cloud browser over the given
// bucket listing. Caller is responsible for fetching `objects` (e.g. via
// [FetchObjects]) so the spinner happens before the alt-screen takes over.
func RunCloudBrowseTUI(objects []cloud.ObjectInfo, client *cloud.Client) error {
	if err := uikit.Run(newCloudBrowseModel(objects, client)); err != nil {
		if errors.Is(err, tea.ErrInterrupted) {
			return ErrInterrupted
		}
		return err
	}
	return nil
}
