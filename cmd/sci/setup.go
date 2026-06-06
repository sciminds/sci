package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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

// setupDoneValue is the sentinel option value for the menu's "Done" entry.
const setupDoneValue = "__done__"

// setupOptions builds the Select option list: one per domain (value == key)
// plus a trailing Done. Kept separate from the interactive loop so it's unit-
// testable without a TTY.
func setupOptions(statuses []domainStatus) []uikit.Option[string] {
	opts := lo.Map(statuses, func(s domainStatus, _ int) uikit.Option[string] {
		mark := uikit.SymFail
		if s.Configured {
			mark = uikit.SymOK
		}
		return uikit.NewOption(fmt.Sprintf("%s  %s — %s", mark, s.Title, s.Summary), s.Key)
	})
	return append(opts, uikit.NewOption("Done", setupDoneValue))
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
	// menu — the non-interactive bypass for the uikit.Select below.
	if cmdutil.IsJSON(cmd) {
		cmdutil.Output(cmd, setupStatusResult{Domains: collectStatuses(entries)})
		return nil
	}

	for {
		choice, err := uikit.Select(
			"Configure sci — pick a tool to set up",
			setupOptions(collectStatuses(entries)),
		)
		if err != nil {
			// Aborting the menu (esc/q/ctrl+c) or running in quiet mode just
			// exits cleanly — nothing was changed.
			if errors.Is(err, uikit.ErrFormAborted) || errors.Is(err, uikit.ErrFormQuiet) {
				return nil
			}
			return err
		}
		if choice == setupDoneValue {
			return nil
		}

		entry, ok := lo.Find(entries, func(e setupEntry) bool { return e.key == choice })
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
