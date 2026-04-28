package cli

// browse.go — interactive TUI for browsing tags and collections.
// Three-level ListPicker navigation (help pattern):
//   Level 0: tag or collection picker
//   Level 1: items for the selected tag/collection
//   Level 2: ActionMenu overlay (Copy BibTeX, Open PDF, Open in Zotero)

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	"github.com/sciminds/cli/internal/zot/local"
	"github.com/urfave/cli/v3"
)

// ── List item types ──────────────────────────────────────────────────

// tagEntry wraps local.Tag for list.Item.
type tagEntry struct{ tag local.Tag }

func (e tagEntry) Title() string       { return e.tag.Name }
func (e tagEntry) Description() string { return fmt.Sprintf("%d items", e.tag.Count) }
func (e tagEntry) FilterValue() string { return e.tag.Name }

// collEntry wraps local.Collection for list.Item.
type collEntry struct{ coll local.Collection }

func (e collEntry) Title() string { return e.coll.Name }
func (e collEntry) Description() string {
	return fmt.Sprintf("%d items · %s", e.coll.ItemCount, e.coll.Key)
}
func (e collEntry) FilterValue() string { return e.coll.Name }

// itemEntry wraps local.Item for the level 1 item list.
type itemEntry struct{ item local.Item }

func (e itemEntry) Title() string {
	author := firstAuthor(e.item.Creators)
	year := extractYear(e.item.Date)

	parts := lo.Compact([]string{author, year})
	prefix := strings.Join(parts, " ")
	if prefix != "" && e.item.Title != "" {
		return prefix + " \u2014 " + e.item.Title
	}
	if e.item.Title != "" {
		return e.item.Title
	}
	return e.item.Key
}

func (e itemEntry) Description() string {
	detail := lo.Ternary(e.item.DOI != "", e.item.DOI, e.item.Key)
	return e.item.Type + " · " + detail
}

func (e itemEntry) FilterValue() string {
	parts := []string{e.item.Title, firstAuthor(e.item.Creators), e.item.DOI}
	return strings.Join(lo.Compact(parts), " ")
}

// firstAuthor returns the first creator's last name, or institutional Name.
func firstAuthor(creators []local.Creator) string {
	if len(creators) == 0 {
		return ""
	}
	c := creators[0]
	if c.Name != "" {
		return c.Name
	}
	return c.Last
}

// extractYear returns the first 4 characters of a Zotero date string
// (format "YYYY-MM-DD ..."), or "" if too short.
func extractYear(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

// ── browseSource ─────────────────────────────────────────────────────

// browseSource abstracts tags vs collections for the shared browse model.
type browseSource struct {
	title     string
	level0    []list.Item
	loadItems func(list.Item) ([]local.Item, error)
}

func newTagBrowseSource(db local.Reader) (browseSource, error) {
	tags, err := db.ListTags()
	if err != nil {
		return browseSource{}, err
	}
	return browseSource{
		title: "Tags",
		level0: uikit.Items(lo.Map(tags, func(t local.Tag, _ int) tagEntry {
			return tagEntry{tag: t}
		})),
		loadItems: func(item list.Item) ([]local.Item, error) {
			te := item.(tagEntry)
			return db.ListAll(local.ListFilter{Tag: te.tag.Name})
		},
	}, nil
}

func newCollBrowseSource(db local.Reader) (browseSource, error) {
	colls, err := db.ListCollections()
	if err != nil {
		return browseSource{}, err
	}
	return browseSource{
		title: "Collections",
		level0: uikit.Items(lo.Map(colls, func(c local.Collection, _ int) collEntry {
			return collEntry{coll: c}
		})),
		loadItems: func(item list.Item) ([]local.Item, error) {
			ce := item.(collEntry)
			return db.ListAll(local.ListFilter{CollectionKey: ce.coll.Key})
		},
	}, nil
}

// ── Action menu builder ──────────────────────────────────────────────

const (
	actionCopyBibTeX = iota
	actionOpenPDF
	actionOpenInZotero
)

// buildActions creates the ActionMenu for an item. Open PDF is disabled
// when no attachment exists.
func buildActions(dataDir string, it *local.Item) uikit.ActionMenu {
	pdfAction := uikit.Action{Name: "Open PDF", Hint: "in default viewer"}
	if zot.PickAttachment(it) == nil {
		pdfAction.Disabled = "no PDF attached"
	}
	title := it.Title
	if len(title) > 50 {
		title = title[:47] + "..."
	}
	return uikit.NewActionMenu(title, []uikit.Action{
		{Name: "Copy BibTeX", Hint: "to clipboard"},
		pdfAction,
		{Name: "Open in Zotero"},
	})
}

// ── Browse model ─────────────────────────────────────────────────────

type browseLevel int

const (
	browseL0 browseLevel = iota // tag/collection picker
	browseL1                    // item list
	browseL2                    // action menu overlay
)

type browseModel struct {
	cfg      *zot.Config
	db       local.Reader
	source   browseSource
	picker   uikit.ListPicker // level 0
	items    uikit.ListPicker // level 1
	actions  uikit.ActionMenu // level 2
	selected *local.Item      // hydrated item for action dispatch
	level    browseLevel
	width    int
	height   int
	quitting bool
}

func newBrowseModel(cfg *zot.Config, db local.Reader, src browseSource) *browseModel {
	lp := uikit.NewListPicker(src.title, src.level0,
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "browse")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	)
	return &browseModel{
		cfg:    cfg,
		db:     db,
		source: src,
		picker: lp,
	}
}

// Init implements tea.Model.
func (m *browseModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		switch m.level {
		case browseL0:
			m.picker.SetSize(msg.Width, msg.Height)
		case browseL1, browseL2:
			m.items.SetSize(msg.Width, msg.Height)
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.level {
		case browseL2:
			return m.updateActions(msg)
		case browseL1:
			return m.updateItems(msg)
		default:
			return m.updatePicker(msg)
		}
	}

	var cmd tea.Cmd
	switch m.level {
	case browseL1:
		m.items, cmd = m.items.Update(msg)
	default:
		m.picker, cmd = m.picker.Update(msg)
	}
	return m, cmd
}

// ── Level 0: tag/collection picker ───────────────────────────────────

func (m *browseModel) updatePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case uikit.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case uikit.KeyQ:
		if !m.picker.IsFiltering() {
			m.quitting = true
			return m, tea.Quit
		}
	case uikit.KeyEnter:
		if m.picker.IsFiltering() {
			break
		}
		sel := m.picker.SelectedItem()
		if sel == nil {
			break
		}
		items, err := m.source.loadItems(sel)
		if err != nil {
			m.picker.StatusMessage(fmt.Sprintf("error: %v", err))
			break
		}
		title := sel.(interface{ Title() string }).Title()
		entries := uikit.Items(lo.Map(items, func(it local.Item, _ int) itemEntry {
			return itemEntry{item: it}
		}))
		m.items = uikit.NewListPicker(title, entries,
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "actions")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		)
		m.items.SetSize(m.width, m.height)
		m.level = browseL1
		return m, nil
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)
	return m, cmd
}

// ── Level 1: item list ───────────────────────────────────────────────

func (m *browseModel) updateItems(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case uikit.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	case uikit.KeyQ:
		if !m.items.IsFiltering() {
			m.quitting = true
			return m, tea.Quit
		}
	case uikit.KeyEsc:
		if !m.items.IsFiltering() {
			m.level = browseL0
			return m, nil
		}
	case uikit.KeyEnter:
		if m.items.IsFiltering() {
			break
		}
		ie, ok := m.items.SelectedItem().(itemEntry)
		if !ok {
			break
		}
		it, err := m.db.Read(ie.item.Key)
		if err != nil {
			m.items.StatusMessage(fmt.Sprintf("error: %v", err))
			break
		}
		m.selected = it
		m.actions = buildActions(m.cfg.DataDir, it)
		m.level = browseL2
		return m, nil
	}

	var cmd tea.Cmd
	m.items, cmd = m.items.Update(msg)
	return m, cmd
}

// ── Level 2: action menu ─────────────────────────────────────────────

func (m *browseModel) updateActions(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case uikit.KeyCtrlC:
		m.quitting = true
		return m, tea.Quit
	}

	m.actions, _ = m.actions.Update(msg)

	if m.actions.Dismissed() {
		m.level = browseL1
		return m, nil
	}

	if idx := m.actions.Picked(); idx >= 0 {
		m.executeAction(idx)
		m.level = browseL1
		return m, nil
	}

	return m, nil
}

func (m *browseModel) executeAction(idx int) {
	switch idx {
	case actionCopyBibTeX:
		bib, err := zot.ExportItem(m.selected, zot.ExportBibTeX)
		if err != nil {
			m.items.StatusMessage(fmt.Sprintf("export error: %v", err))
			return
		}
		if err := clipboard.WriteAll(bib); err != nil {
			m.items.StatusMessage(fmt.Sprintf("clipboard error: %v", err))
			return
		}
		m.items.StatusMessage("Copied BibTeX to clipboard")

	case actionOpenPDF:
		att := zot.PickAttachment(m.selected)
		if att == nil {
			m.items.StatusMessage("no PDF attached")
			return
		}
		path := zot.AttachmentPath(m.cfg.DataDir, att)
		if err := zot.LaunchFile(path); err != nil {
			m.items.StatusMessage(fmt.Sprintf("open error: %v", err))
			return
		}
		m.items.StatusMessage("Opened PDF")

	case actionOpenInZotero:
		uri := "zotero://select/library/items/" + m.selected.Key
		if err := zot.LaunchFile(uri); err != nil {
			m.items.StatusMessage(fmt.Sprintf("open error: %v", err))
			return
		}
		m.items.StatusMessage("Opened in Zotero")
	}
}

// ── View ─────────────────────────────────────────────────────────────

func (m *browseModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	var bg string
	switch m.level {
	case browseL0:
		bg = m.picker.View()
	case browseL1, browseL2:
		bg = m.items.View()
	}

	if m.level == browseL2 {
		fg := m.actions.View(m.width, m.height)
		v := tea.NewView(uikit.Compose(fg, bg))
		v.AltScreen = true
		return v
	}

	v := tea.NewView(bg)
	v.AltScreen = true
	return v
}

// ── CLI commands ─────────────────────────────────────────────────────

func tagsBrowseCommand() *cli.Command {
	return &cli.Command{
		Name:        "browse",
		Usage:       "Interactively browse tags and their items",
		Description: "$ sci zot tags browse",
		Action: func(ctx context.Context, _ *cli.Command) error {
			cfg, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			src, err := newTagBrowseSource(db)
			if err != nil {
				return err
			}
			return uikit.Run(newBrowseModel(cfg, db, src))
		},
	}
}

func collBrowseCommand() *cli.Command {
	return &cli.Command{
		Name:        "browse",
		Usage:       "Interactively browse collections and their items",
		Description: "$ sci zot collection browse",
		Action: func(ctx context.Context, _ *cli.Command) error {
			cfg, db, err := openLocalDB(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
			src, err := newCollBrowseSource(db)
			if err != nil {
				return err
			}
			return uikit.Run(newBrowseModel(cfg, db, src))
		},
	}
}
