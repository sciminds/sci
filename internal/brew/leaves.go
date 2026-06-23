package brew

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"
)

// installReceipt is the subset of Homebrew's INSTALL_RECEIPT.json we need to
// reconstruct `brew leaves --installed-on-request` ourselves. Name is not in
// the JSON — it's the Cellar directory the receipt lives under, filled in by
// [readInstallReceipts].
type installReceipt struct {
	Name                string        `json:"-"`
	InstalledOnRequest  bool          `json:"installed_on_request"`
	Source              receiptSource `json:"source"`
	RuntimeDependencies []receiptDep  `json:"runtime_dependencies"`
}

type receiptSource struct {
	Tap string `json:"tap"`
}

type receiptDep struct {
	FullName string `json:"full_name"`
}

// fullName returns the tap-qualified name (e.g. "oven-sh/bun/bun") for formulae
// from a third-party tap, so the emitted Brewfile entry stays installable —
// `brew install bun` fails because bun is not in homebrew-core. Core formulae
// keep their bare name.
func (r installReceipt) fullName() string {
	if r.Source.Tap != "" && r.Source.Tap != "homebrew/core" {
		return r.Source.Tap + "/" + r.Name
	}
	return r.Name
}

// Leaves implements Runner. Returns user-requested formulae (installed on
// request) that nothing else depends on — the equivalent of
// `brew leaves --installed-on-request`, but derived from the Cellar's install
// receipts rather than the `brew leaves` command.
//
// We don't shell out to `brew leaves`: Homebrew 6.x silently drops tap-sourced
// formulae from `brew leaves` (and from every other brew metadata command that
// resolves taps — `list --full-name`, `info --json --installed`), so a formula
// the user installed via `brew install` from a tap would never be captured into
// their Brewfile. Each formula's INSTALL_RECEIPT.json is the only complete
// source and works identically on Homebrew 5.x and 6.x.
func (CLI) Leaves() ([]string, error) {
	out, err := runBrewOutputLocal("--cellar")
	if err != nil {
		return nil, err
	}
	return leavesFromCellar(strings.TrimSpace(out)), nil
}

// leavesFromCellar reads every install receipt under cellar and computes the
// requested-leaf set. Split out from [CLI.Leaves] so it's testable against a
// fake Cellar without shelling out to brew.
func leavesFromCellar(cellar string) []string {
	return computeRequestedLeaves(readInstallReceipts(cellar))
}

// readInstallReceipts parses every <cellar>/<name>/<version>/INSTALL_RECEIPT.json,
// tagging each with its formula name (the Cellar directory). Unreadable or
// malformed individual receipts are skipped rather than failing the whole scan,
// so one bad file can't break a Brewfile sync.
func readInstallReceipts(cellar string) []installReceipt {
	matches, err := filepath.Glob(filepath.Join(cellar, "*", "*", "INSTALL_RECEIPT.json"))
	if err != nil {
		return nil
	}
	return lo.FilterMap(matches, func(path string, _ int) (installReceipt, bool) {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return installReceipt{}, false
		}
		var r installReceipt
		if json.Unmarshal(data, &r) != nil {
			return installReceipt{}, false
		}
		// <cellar>/<name>/<version>/INSTALL_RECEIPT.json -> name is two dirs up.
		r.Name = filepath.Base(filepath.Dir(filepath.Dir(path)))
		return r, true
	})
}

// computeRequestedLeaves returns the tap-qualified names of formulae that were
// installed on request and are not a runtime dependency of any installed
// formula — matching `brew leaves --installed-on-request` semantics.
func computeRequestedLeaves(receipts []installReceipt) []string {
	dependedOn := make(map[string]bool)
	for _, r := range receipts {
		for _, d := range r.RuntimeDependencies {
			dependedOn[shortFormula(d.FullName)] = true
		}
	}
	leaves := lo.FilterMap(receipts, func(r installReceipt, _ int) (string, bool) {
		return r.fullName(), r.InstalledOnRequest && !dependedOn[shortFormula(r.Name)]
	})
	slices.Sort(leaves)
	return slices.Compact(leaves)
}
