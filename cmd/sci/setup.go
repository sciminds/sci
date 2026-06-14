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
	run    func(context.Context, *cli.Command) error // the domain's setup flow
}

// setupRegistry lists every domain the top-level menu can configure. Adding a
// tool here (lab, zot today; cass, cloud, ts next) is all it takes to surface it
// in `sci setup`. Kept as a function (not a package var) so each invocation
// re-reads current on-disk status.
func setupRegistry() []setupEntry {
	return []setupEntry{
		{key: "lab", title: "Lab storage (SSH/SFTP)", status: labSetupStatus, run: runLabSetup},
		{key: "zot", title: "Zotero library", status: zotSetupStatus, run: zotcli.RunSetup},
	}
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

// menuItem is one row in the interactive `sci setup` menu. It implements
// list.Item so it can live in the shared ListPicker, carrying the domain key
// through to the selection handler.
type menuItem struct {
	key     string
	title   string
	summary string
	ok      bool
}

// Title renders the row's headline: a ✓/✗ status mark plus the domain title.
func (mi menuItem) Title() string {
	mark := uikit.SymFail
	if mi.ok {
		mark = uikit.SymOK
	}
	return mark + "  " + mi.title
}

// Description renders the dimmed summary line under the title.
func (mi menuItem) Description() string { return mi.summary }

// FilterValue is what `/` filters against.
func (mi menuItem) FilterValue() string { return mi.title }

// menuItems builds the list rows from a status snapshot. Kept separate from
// the model so it's unit-testable without a TTY.
func menuItems(statuses []domainStatus) []list.Item {
	return lo.Map(statuses, func(s domainStatus, _ int) list.Item {
		return menuItem{key: s.Key, title: s.Title, summary: s.Summary, ok: s.Configured}
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
		// when the menu re-opens.
		menu, err := uikit.RunModel(newSetupMenu(collectStatuses(entries)))
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

// setupMenuModel is the interactive `sci setup` menu — a thin tea.Model over
// the shared ListPicker. It runs once per loop pass: opening a row records the
// pick and quits so the caller can run that tool's setup, then re-launch.
//
// Svelte lens: a small component whose state is the embedded list plus the
// user's pick; Update is its event handler, View its render.
type setupMenuModel struct {
	list     uikit.ListPicker
	picked   string // domain key chosen via IntentOpen
	pickedOK bool   // false when the user quit without choosing
	quitting bool
}

// newSetupMenu builds the menu model from a status snapshot.
func newSetupMenu(statuses []domainStatus) *setupMenuModel {
	lp := uikit.NewListPicker("Configure sci — pick a tool to set up", menuItems(statuses))
	return &setupMenuModel{list: lp}
}

// Init implements tea.Model.
func (m *setupMenuModel) Init() tea.Cmd { return nil }

// Update implements tea.Model, routing navigation through the shared keymap so
// enter/l opens and q/esc exits — consistent with help, learn, and cloud.
func (m *setupMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyPressMsg:
		switch m.list.Classify(msg) {
		case uikit.IntentQuit, uikit.IntentBack:
			m.quitting = true
			return m, tea.Quit
		case uikit.IntentOpen:
			if it, ok := m.list.SelectedItem().(menuItem); ok {
				m.picked = it.key
				m.pickedOK = true
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
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
