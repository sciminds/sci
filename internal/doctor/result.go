package doctor

// result.go — [DocResult] implements [cmdutil.Result] (JSON + Human output)
// for the `sci doctor` command.

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/sciminds/sci/internal/brew"
	"github.com/sciminds/sci/internal/uikit"
)

// DocResult is the top-level result returned by `sci doctor`.
// It implements [cmdutil.Result] via [JSON] and [Human].
type DocResult struct {
	Sections []CheckSection
	Tools    []ToolInfo

	// Brewfile state — populated when the full flow runs (including --json).
	BrewfilePath    string
	BrewfileCreated bool
	PackagesAdded   []string
	AppendError     string
	ToolCheckError  string
	ToolsInstalled  []string
	InstallError    string

	// Update state — populated after the outdated-package check.
	Outdated    []brew.OutdatedPackage
	Upgraded    []brew.OutdatedPackage
	UpdateError string

	// UpgradeCheck says what happened to the brew/uv outdated check itself.
	// Always emitted, so a consumer can tell "nothing is outdated" from
	// "we never looked" without inferring it from an empty list.
	UpgradeCheck UpgradeCheckReport
}

// InstallErrorHint is the remediation attached to every InstallError. sci
// only sees brew's exit status, not which formula or step failed, so the
// advice targets the common cause of a bottle that pours but whose
// post-install step fails: a Homebrew that has not self-updated (a formula
// can adopt install-step DSL its brew does not know yet, and a runner image
// or HOMEBREW_NO_AUTO_UPDATE keeps brew from catching up on its own).
// `brew update` is what a self-updating brew would have done first, and
// re-running doctor retries the install.
const InstallErrorHint = "run `brew update` then re-run `sci doctor` — a stale Homebrew can fail a " +
	"formula's post-install step; `brew postinstall <formula>` retries just that step"

// UpgradeCheckStatus is the closed vocabulary for the fate of the brew/uv
// outdated check.
type UpgradeCheckStatus string

// Outcomes of the brew/uv outdated check.
const (
	// UpgradeCheckOK means the check completed; Outdated is authoritative.
	UpgradeCheckOK UpgradeCheckStatus = "ok"
	// UpgradeCheckSkipped means the check was deliberately not run
	// (--skip-upgrade-check, SCI_SKIP_UPGRADE_CHECK, or --json without --yes).
	UpgradeCheckSkipped UpgradeCheckStatus = "skipped"
	// UpgradeCheckTimedOut means the check was abandoned at its deadline.
	// The upgrade picture is unknown, not clean.
	UpgradeCheckTimedOut UpgradeCheckStatus = "timed_out"
	// UpgradeCheckFailed means brew or uv returned an error.
	UpgradeCheckFailed UpgradeCheckStatus = "failed"
)

// UpgradeCheckReport describes the outcome of the brew/uv outdated check in a
// form an agent can branch on: a [UpgradeCheckStatus] from a closed set, the
// underlying reason when there is one, and the command that would produce the
// missing answer.
type UpgradeCheckReport struct {
	Status UpgradeCheckStatus `json:"status"`
	Reason string             `json:"reason,omitempty"`
	Hint   string             `json:"hint,omitempty"`
}

type jsonCheck struct {
	Label   string `json:"label"`
	Status  Status `json:"status"`
	Message string `json:"message"`
}

type jsonSection struct {
	Name   string      `json:"name"`
	Checks []jsonCheck `json:"checks"`
}

type jsonSummary struct {
	Pass int `json:"pass"`
	Fail int `json:"fail"`
	Warn int `json:"warn"`
}

type jsonTool struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
}

type jsonDocResult struct {
	Sections        []jsonSection          `json:"sections"`
	Summary         jsonSummary            `json:"summary"`
	Tools           []jsonTool             `json:"tools,omitempty"`
	BrewfilePath    string                 `json:"brewfile_path,omitempty"`
	BrewfileCreated bool                   `json:"brewfile_created,omitempty"`
	PackagesAdded   []string               `json:"packages_added,omitempty"`
	AppendError     string                 `json:"append_error,omitempty"`
	ToolCheckError  string                 `json:"tool_check_error,omitempty"`
	ToolsInstalled  []string               `json:"tools_installed,omitempty"`
	InstallError    string                 `json:"install_error,omitempty"`
	InstallHint     string                 `json:"install_hint,omitempty"`
	Outdated        []brew.OutdatedPackage `json:"outdated,omitempty"`
	Upgraded        []brew.OutdatedPackage `json:"upgraded,omitempty"`
	UpdateError     string                 `json:"update_error,omitempty"`
	UpgradeCheck    UpgradeCheckReport     `json:"upgrade_check"`
}

// normalized fills in a status for a report nobody set. Every code path that
// returns before the outdated check runs — Linux, an offline machine, a bail
// on a missing Homebrew — is a skip, and saying so beats emitting an empty
// status a consumer has to guess about.
func (u UpgradeCheckReport) normalized() UpgradeCheckReport {
	if u.Status == "" {
		u.Status = UpgradeCheckSkipped
		u.Reason = cmp.Or(u.Reason, "the upgrade check did not run")
	}
	return u
}

// JSON returns the structured output for --json mode.
func (r DocResult) JSON() any {
	upgradeCheck := r.UpgradeCheck.normalized()
	pass, fail, warn := 0, 0, 0
	sections := make([]jsonSection, 0, len(r.Sections))
	for _, sec := range r.Sections {
		js := jsonSection{Name: sec.Name}
		for _, c := range sec.Checks {
			js.Checks = append(js.Checks, jsonCheck(c))
			switch c.Status {
			case StatusFail:
				fail++
			case StatusWarn:
				warn++
			default:
				pass++
			}
		}
		sections = append(sections, js)
	}
	var tools []jsonTool
	for _, t := range r.Tools {
		tools = append(tools, jsonTool(t))
	}

	return jsonDocResult{
		Sections:        sections,
		Summary:         jsonSummary{Pass: pass, Fail: fail, Warn: warn},
		Tools:           tools,
		BrewfilePath:    r.BrewfilePath,
		BrewfileCreated: r.BrewfileCreated,
		PackagesAdded:   r.PackagesAdded,
		AppendError:     r.AppendError,
		ToolCheckError:  r.ToolCheckError,
		ToolsInstalled:  r.ToolsInstalled,
		InstallError:    r.InstallError,
		InstallHint:     r.installHint(),
		Outdated:        r.Outdated,
		Upgraded:        r.Upgraded,
		UpdateError:     r.UpdateError,
		UpgradeCheck:    upgradeCheck,
	}
}

// Human returns the styled terminal output.
func (r DocResult) Human() string {
	var b strings.Builder
	pass, fail, warn := 0, 0, 0

	for _, sec := range r.Sections {
		fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.Bold().Render(sec.Name))
		for _, c := range sec.Checks {
			sym := uikit.SymOK
			switch c.Status {
			case StatusFail:
				sym = uikit.SymFail
				fail++
			case StatusWarn:
				sym = uikit.SymWarn
				warn++
			default:
				pass++
			}
			fmt.Fprintf(&b, "    %s %-20s %s\n", sym, c.Label, uikit.TUI.Dim().Render(c.Message))
		}
	}

	// Tools summary (one line instead of full list)
	if r.ToolCheckError != "" {
		fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.Bold().Render("Tools"))
		fmt.Fprintf(&b, "    %s %-20s %s\n", uikit.SymWarn, "status",
			uikit.TUI.Warn().Render("could not check: "+r.ToolCheckError))
	} else if len(r.Tools) > 0 {
		installed := lo.CountBy(r.Tools, func(t ToolInfo) bool {
			return t.Installed
		})
		sym := lo.Ternary(installed < len(r.Tools), uikit.SymFail, uikit.SymOK)
		fmt.Fprintf(&b, "\n  %s\n", uikit.TUI.Bold().Render("Tools"))
		fmt.Fprintf(&b, "    %s %-20s %s\n", sym, "installed",
			uikit.TUI.Dim().Render(fmt.Sprintf("%d/%d", installed, len(r.Tools))))
	}

	// A failed install is reported with its remedy, never left as a bare
	// exit status the reader has to diagnose from brew's scrollback.
	if r.InstallError != "" {
		fmt.Fprintf(&b, "\n  %s %s\n", uikit.SymFail,
			uikit.TUI.Fail().Render("Install failed: "+r.InstallError))
		fmt.Fprintf(&b, "    %s try: %s\n", uikit.SymArrow, r.installHint())
	}

	// An abandoned or failed upgrade check is reported, never silently
	// rendered as "nothing outdated".
	switch r.UpgradeCheck.Status {
	case UpgradeCheckTimedOut:
		fmt.Fprintf(&b, "\n  %s %s\n", uikit.SymWarn,
			uikit.TUI.Warn().Render("Upgrade check timed out — package updates unknown"))
	case UpgradeCheckFailed:
		fmt.Fprintf(&b, "\n  %s %s\n", uikit.SymWarn,
			uikit.TUI.Warn().Render("Upgrade check failed: "+r.UpgradeCheck.Reason))
	}

	// Summary line — only shown when there are problems.
	if fail > 0 || warn > 0 {
		var parts []string
		if fail > 0 {
			parts = append(parts, uikit.TUI.Fail().Render(fmt.Sprintf("%d failed", fail)))
		}
		if warn > 0 {
			parts = append(parts, uikit.TUI.Warn().Render(fmt.Sprintf("%d warnings", warn)))
		}
		fmt.Fprintf(&b, "\n  %s\n", strings.Join(parts, uikit.TUI.Dim().Render(" · ")))
	}
	fmt.Fprintln(&b)

	return b.String()
}

// installHint returns [InstallErrorHint] when an install failed and "" otherwise,
// so the JSON field is omitted (and no advice is rendered) when nothing failed.
func (r DocResult) installHint() string {
	if r.InstallError == "" {
		return ""
	}
	return InstallErrorHint
}

// AllPassed returns true if no checks failed.
func (r DocResult) AllPassed() bool {
	for _, sec := range r.Sections {
		for _, c := range sec.Checks {
			if c.Status == StatusFail {
				return false
			}
		}
	}
	return true
}
