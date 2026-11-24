# `sci tools ignore` — design

Issue: [#5](https://github.com/sciminds/sci/issues/5) · Status: approved 2026-11-23

## Intent

A user who updates some Homebrew packages by hand can exclude them from every
upgrade sci runs. `brew upgrade foo` keeps working on an ignored package.

**Goals:** ignore formulae, casks, and uv tools; `tools update`, `tools outdated`,
and `doctor` all respect the list; ignored updates stay visible.

**Non-goals:** changing Brewfile sync, `tools install`, or `tools uninstall`;
syncing the list between machines; managing `brew pin`.

**Failure would be:** sci directly upgrading an ignored package, or ignoring
silently hiding that an update exists.

## Constraints

- Formulae and casks can share a name, so an entry is identified by type and name.
- Formula names compare on their bare form (`shortFormula`): Homebrew 5.x reports
  tap formulae by full name and 6.x by bare name.
- Known limit, stated in the command help: brew may still upgrade an ignored
  formula as a dependency or dependent of another package.

## Approaches

| | Trade-off |
|---|---|
| **sci config file (chosen)** | Follows `sciconfig`, covers packages outside the Brewfile, survives sync; per machine |
| Brewfile annotation | Visible and portable; lost on sync or rewrite, Brewfile packages only |
| `brew pin` | Enforced by brew; blocks `brew upgrade foo`, no uv support |

## Design (decided)

**Storage.** `~/.config/sci/tools.json`:
`{"ignored": [{"type": "cask", "name": "libreoffice"}]}`.
Types are `formula`, `cask`, `uv`.

**Commands.**
- `sci tools ignore` lists entries.
- `sci tools ignore <package>` adds one. It must be installed. If the name matches
  several types, `--formula`, `--cask`, or `--uv` is required.
- `sci tools unignore <package>` removes one, installed or not, with the same flags.
- Both honor `--dry-run` and `--json` and work offline.

**Checking.** The shared outdated check splits its result into upgradable, ignored,
and pinned packages. Cask pins are read as well as formula pins.

**Upgrading.** sci upgrades the upgradable set by name, with formulae and casks in
separate brew calls. An empty set makes no call; it never falls back to a bare
`brew upgrade`. This covers `tools update`, the `tools outdated` and interactive
`doctor` prompts, and `doctor --json --yes`.

**Output.** `outdated` lists only upgradable packages; JSON adds `ignored` and
`pinned`. Human output lists both sets below the outdated packages, and shows them
even when nothing else is outdated.

**Errors.** An unreadable or malformed `tools.json` fails the check before any
upgrade and names the file. A missing file means nothing is ignored.

## Builder's calls (correct any of these)

- Ignoring an ignored package, or unignoring one that is not, succeeds and says so.
- Uninstalling a package leaves its ignore entry in place.
- Outdated packages gain a `type` field in JSON.
- `sci tools install foo` still acts on an ignored package, since it was named.

## Left to the builder

Package layout, function signatures, output styling, test structure. Tests must
cover all four upgrade paths, a formula and cask sharing a name, tap-qualified
names, the all-ignored case, and a malformed list. README and help are updated.
