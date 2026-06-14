package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cass"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/lab"
	"github.com/sciminds/cli/internal/uikit"
	"github.com/sciminds/cli/internal/zot"
	zotcli "github.com/sciminds/cli/internal/zot/cli"
	"github.com/urfave/cli/v3"
)

// setupEntry is one configurable domain in the top-level `sci setup` menu.
//
// run delegates to that domain's existing setup flow — `sci setup` is a single
// front door over the per-command setups, not a second implementation. Each
// domain's `sci <cmd> setup` keeps working unchanged.
type setupEntry struct {
	key    string                                    // stable id, also the Select option value
	title  string                                    // human label in the menu
	status func() (configured bool, summary string)  // cheap, local (no-network) snapshot
	fields func() []fieldRow                         // current config values, for the drill-in view
	run    func(context.Context, *cli.Command) error // the domain's setup flow
}

// fieldRow is one configurable value shown when the user drills into a tool.
// It is purely informational: the drill-in lists every settable item with its
// current value so the user can eyeball the config at a glance. Selecting any
// row launches the tool's full setup wizard (the values themselves are not
// edited inline — provisioning and validation live in the wizard).
type fieldRow struct {
	label string // config key as the user knows it (e.g. "user", "api_key")
	value string // current value, or "(not set)" when empty
}

// orNotSet renders an empty config value as a dim "(not set)" placeholder so
// the drill-in never shows a blank line.
func orNotSet(v string) string {
	if v == "" {
		return "(not set)"
	}
	return v
}

// setupRegistry lists every domain the top-level menu can configure. Adding a
// tool here (lab, zot today; cass, cloud, ts next) is all it takes to surface it
// in `sci setup`. Kept as a function (not a package var) so each invocation
// re-reads current on-disk status.
func setupRegistry() []setupEntry {
	return []setupEntry{
		{key: "lab", title: "Lab storage (SSH/SFTP)", status: labSetupStatus, fields: labFields, run: runLabSetup},
		{key: "zot", title: "Zotero library", status: zotSetupStatus, fields: zotFields, run: zotcli.RunSetup},
		{key: "cass", title: "Canvas LMS (cass)", status: cassSetupStatus, fields: cassFields, run: func(ctx context.Context, cmd *cli.Command) error {
			return runCassSetup(ctx, cmd, "")
		}},
	}
}

// labFields lists the lab storage config values for the drill-in view.
func labFields() []fieldRow {
	cfg, _ := lab.LoadConfig()
	user := ""
	if cfg != nil {
		user = cfg.User
	}
	return []fieldRow{
		{label: "user", value: orNotSet(user)},
	}
}

// zotFields lists the Zotero config values for the drill-in view. Credentials
// are shown in plaintext — this is a local, single-user config viewer and the
// point is to read the values back quickly.
func zotFields() []fieldRow {
	cfg, _ := zot.LoadConfig()
	if cfg == nil {
		cfg = &zot.Config{}
	}
	sharedGroup := cfg.SharedGroupName
	if sharedGroup != "" && cfg.SharedGroupID != "" {
		sharedGroup += " (" + cfg.SharedGroupID + ")"
	} else if sharedGroup == "" {
		sharedGroup = cfg.SharedGroupID
	}
	return []fieldRow{
		{label: "api_key", value: orNotSet(cfg.APIKey)},
		{label: "user_id", value: orNotSet(cfg.UserID)},
		{label: "shared_group", value: orNotSet(sharedGroup)},
		{label: "data_dir", value: orNotSet(cfg.DataDir)},
		{label: "openalex_email", value: orNotSet(cfg.OpenAlexEmail)},
		{label: "openalex_api_key", value: orNotSet(cfg.OpenAlexAPIKey)},
	}
}

// cassFields lists the cass (Canvas) config values for the drill-in view. The
// Canvas API token is a bearer credential, so it is masked to a short prefix —
// enough to recognise it without printing it in full.
func cassFields() []fieldRow {
	token, _ := cass.LoadCanvasToken(cass.CredentialsPath())
	return []fieldRow{
		{label: "canvas_token", value: orNotSet(maskToken(token))},
	}
}

// maskToken renders a bearer token as a short recognisable prefix, never in
// full. An empty token returns "" so [orNotSet] shows the "(not set)" placeholder.
func maskToken(t string) string {
	if len(t) > 8 {
		return t[:8] + "…"
	}
	return t
}

// labSetupStatus reports whether lab storage is configured, reading only local
// config (no SSH probe — the menu must stay instant to open).
func labSetupStatus() (bool, string) {
	cfg, _ := lab.LoadConfig()
	if cfg != nil && cfg.User != "" {
		return true, "user " + cfg.User + "@" + lab.Host
	}
	return false, "not configured"
}

// zotSetupStatus reports Zotero config state. A file present but missing the
// required credentials reads as "incomplete" rather than configured.
func zotSetupStatus() (bool, string) {
	cfg, _ := zot.LoadConfig()
	switch {
	case cfg != nil && cfg.APIKey != "" && cfg.UserID != "":
		summary := "user ID " + cfg.UserID
		if cfg.SharedGroupName != "" {
			summary += " · shared: " + cfg.SharedGroupName
		}
		return true, summary
	case cfg != nil:
		return false, "incomplete — re-run setup"
	default:
		return false, "not configured"
	}
}

// cassSetupStatus reports whether the Canvas API token is configured, reading
// only the local credentials file (no Canvas API probe — the menu stays instant).
func cassSetupStatus() (bool, string) {
	token, _ := cass.LoadCanvasToken(cass.CredentialsPath())
	if token != "" {
		return true, "Canvas API token saved"
	}
	return false, "not configured"
}

// domainStatus is the JSON/Human shape for one domain's configuration state,
// emitted by `sci setup --json`.
type domainStatus struct {
	Key        string `json:"key"`
	Title      string `json:"title"`
	Configured bool   `json:"configured"`
	Summary    string `json:"summary"`
}

// collectStatuses snapshots every registered domain's current state.
func collectStatuses(entries []setupEntry) []domainStatus {
	return lo.Map(entries, func(e setupEntry, _ int) domainStatus {
		configured, summary := e.status()
		return domainStatus{Key: e.key, Title: e.title, Configured: configured, Summary: summary}
	})
}

// menuItem is one tool row in the top level of the interactive `sci setup`
// menu. It implements list.Item so it can live in the shared ListPicker,
// carrying the domain key through to the selection handler. The per-tool
// summary that used to render under the title now lives one level down, in the
// drill-in field list ([fieldItem]).
type menuItem struct {
	key   string
	title string
	ok    bool
}

// Title renders the row: a ✓/✗ status mark plus the domain title.
func (mi menuItem) Title() string {
	mark := uikit.SymFail
	if mi.ok {
		mark = uikit.SymOK
	}
	return mark + "  " + mi.title
}

// Description is empty — the top-level menu renders single-line rows via
// [uikit.NewCompactListPicker], so there is no summary line under the title.
func (mi menuItem) Description() string { return "" }

// FilterValue is what `/` filters against.
func (mi menuItem) FilterValue() string { return mi.title }

// toolItems builds the top-level tool rows from the registry, snapshotting each
// tool's configured/not-configured state for the ✓/✗ mark.
func toolItems(entries []setupEntry) []list.Item {
	return lo.Map(entries, func(e setupEntry, _ int) list.Item {
		ok, _ := e.status()
		return menuItem{key: e.key, title: e.title, ok: ok}
	})
}

// fieldItem is one config row in a tool's drill-in view: a label and its
// current value. It is informational only — opening it (enter/l) launches the
// tool's setup wizard, identified by toolKey.
type fieldItem struct {
	toolKey string
	label   string
	value   string
}

// Title renders the field as "label  value", with the value dimmed.
func (fi fieldItem) Title() string {
	return fmt.Sprintf("%-18s %s", fi.label, uikit.TUI.Dim().Render(fi.value))
}

// Description is empty — the drill-in renders single-line rows.
func (fi fieldItem) Description() string { return "" }

// FilterValue filters on the field label.
func (fi fieldItem) FilterValue() string { return fi.label }

// fieldItems builds the drill-in rows for one tool from its current config.
func fieldItems(e setupEntry) []list.Item {
	return lo.Map(e.fields(), func(f fieldRow, _ int) list.Item {
		return fieldItem{toolKey: e.key, label: f.label, value: f.value}
	})
}

// setupStatusResult is the --json payload for `sci setup`.
type setupStatusResult struct {
	Domains []domainStatus `json:"domains"`
}

func (r setupStatusResult) JSON() any { return r }

func (r setupStatusResult) Human() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n  %s\n\n", uikit.TUI.Bold().Render("sci configuration"))
	for _, d := range r.Domains {
		mark := uikit.SymFail
		if d.Configured {
			mark = uikit.SymOK
		}
		fmt.Fprintf(&b, "  %s  %-24s %s\n", mark, d.Title, uikit.TUI.Dim().Render(d.Summary))
	}
	return b.String()
}

func setupCommand() *cli.Command {
	return &cli.Command{
		Name:     "setup",
		Usage:    "Configure sci tools (lab, zot, …) from one place",
		Category: "Getting Started",
		Description: "$ sci setup            # interactive menu — see status, pick a tool to configure\n" +
			"$ sci setup --json     # print configuration status (non-interactive)\n\n" +
			"Each tool still has its own setup (e.g. `sci lab setup`); this is one front door over all of them.",
		Action: runSetupMenu,
	}
}

func runSetupMenu(ctx context.Context, cmd *cli.Command) error {
	entries := setupRegistry()

	// --json prints a status snapshot rather than launching the interactive
	// menu — the non-interactive bypass for the list picker below.
	if cmdutil.IsJSON(cmd) {
		cmdutil.Output(cmd, setupStatusResult{Domains: collectStatuses(entries)})
		return nil
	}

	for {
		// Re-snapshot status each pass so a just-configured tool shows its ✓
		// when the menu re-opens. A fresh model also reopens at the top
		// (tool) level after a wizard runs, which is the natural place to land.
		menu, err := uikit.RunModel(newSetupMenu(entries))
		if err != nil {
			return err
		}
		if !menu.pickedOK {
			return nil // q/esc — nothing changed
		}

		entry, ok := lo.Find(entries, func(e setupEntry) bool { return e.key == menu.picked })
		if !ok {
			continue
		}

		uikit.Header("Setting up " + entry.title)
		if err := entry.run(ctx, cmd); err != nil {
			// Cancelling a sub-flow returns to the menu quietly; any real
			// failure is surfaced but still keeps the menu open so the user can
			// fix another tool or retry.
			if errors.Is(err, uikit.ErrFormAborted) {
				uikit.Hint("cancelled — back to menu")
				continue
			}
			fmt.Fprintf(os.Stderr, "  %s %s\n", uikit.SymFail, err)
		}
	}
}

// menuLevel is which of the two-level `sci setup` menu the user is currently
// looking at: the list of tools, or one tool's config fields.
type menuLevel int

const (
	levelTools  menuLevel = iota // top: pick a tool to drill into
	levelFields                  // inside a tool: view its config, pick any row to run setup
)

// setupMenuModel is the interactive `sci setup` menu — a thin tea.Model over
// the shared ListPicker with two levels. The top level lists tools; opening a
// tool (enter/l) drills into a read-only view of its config fields; opening any
// field records the tool pick and quits so the caller can run that tool's setup
// wizard, then re-launch. esc/h backs out a level (or quits from the top).
//
// Svelte lens: a small component whose state is the embedded list, the current
// level, and the user's pick; Update is its event handler, View its render.
type setupMenuModel struct {
	entries  []setupEntry // registry, so a drill-in can build the field list
	list     uikit.ListPicker
	level    menuLevel
	picked   string // domain key chosen by opening a field row
	pickedOK bool   // false when the user quit without choosing
	quitting bool
}

const setupTitle = "Configure sci — pick a tool"

// newSetupMenu builds the menu model at the top (tool) level.
func newSetupMenu(entries []setupEntry) *setupMenuModel {
	lp := uikit.NewCompactListPicker(setupTitle, toolItems(entries))
	return &setupMenuModel{entries: entries, list: lp}
}

// Init implements tea.Model.
func (m *setupMenuModel) Init() tea.Cmd { return nil }

// Update implements tea.Model, routing navigation through the shared keymap so
// enter/l opens and q/esc/h exits or backs out — consistent with help, learn,
// and cloud.
func (m *setupMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyPressMsg:
		switch m.list.Classify(msg) {
		case uikit.IntentQuit:
			m.quitting = true
			return m, tea.Quit
		case uikit.IntentBack:
			// esc/h backs out of a tool's fields to the tool list; from the
			// top level it quits.
			if m.level == levelFields {
				m.showTools()
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		case uikit.IntentOpen:
			return m.open()
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// open handles enter/l: at the top level it drills into the highlighted tool's
// fields; inside a tool it records the pick and quits so the caller runs that
// tool's setup wizard.
func (m *setupMenuModel) open() (tea.Model, tea.Cmd) {
	if m.level == levelTools {
		it, ok := m.list.SelectedItem().(menuItem)
		if !ok {
			return m, nil
		}
		entry, ok := lo.Find(m.entries, func(e setupEntry) bool { return e.key == it.key })
		if !ok {
			return m, nil
		}
		m.showFields(entry)
		return m, nil
	}
	if fi, ok := m.list.SelectedItem().(fieldItem); ok {
		m.picked = fi.toolKey
		m.pickedOK = true
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// showFields swaps the list into the tool's drill-in (config field) view.
func (m *setupMenuModel) showFields(e setupEntry) {
	m.level = levelFields
	m.list.SetItems(fieldItems(e))
	m.list.SetTitle(e.title + " — select any field to run setup")
	m.list.ResetSelected()
}

// showTools swaps the list back to the top-level tool view, re-snapshotting
// status so a just-changed tool shows the right ✓/✗ mark.
func (m *setupMenuModel) showTools() {
	m.level = levelTools
	m.list.SetItems(toolItems(m.entries))
	m.list.SetTitle(setupTitle)
	m.list.ResetSelected()
}

// View implements tea.Model.
func (m *setupMenuModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	v := tea.NewView(m.list.View())
	v.AltScreen = true
	return v
}
