#!/usr/bin/env bash
# lint-guard.sh — project-specific structural checks that complement golangci-lint
# and ast-grep. Enforces import boundaries, flag conventions, and API usage rules
# documented in CLAUDE.md. Run via `just lint-guard`.
set -euo pipefail

errors=0
# Accumulate rule names so the summary tells you which fired.
failed_rules=()

fail() {
	errors=$((errors + 1))
	# Deduplicate: only add rule name if not already present.
	local rule="$1"
	for r in "${failed_rules[@]+"${failed_rules[@]}"}"; do
		[[ "$r" == "$rule" ]] && return
	done
	failed_rules+=("$rule")
}

# ── Rule 1: No v1 bubbletea / bubbles / lipgloss / huh imports ──────────────
# v2 paths: charm.land/{pkg}/v2  (or github.com/charmbracelet/{pkg}/v2 historically)
# v1 would be the same path WITHOUT /v2, or the old github.com/charmbracelet/ path
# for a direct (non-indirect) import.
#
# We check for any Go import of the charm.land packages without the /v2 suffix,
# and for any github.com/charmbracelet/{bubbletea,bubbles,lipgloss,huh} import
# (which would be the pre-migration v1 path).

v1_hits=$(rg -n '"charm\.land/(bubbletea|bubbles|lipgloss|huh)"' \
	--type go --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null || true)
if [[ -n "$v1_hits" ]]; then
	echo "FAIL [no-v1-charm] v1 charm.land import (missing /v2 suffix):"
	echo "$v1_hits"
	fail "no-v1-charm"
fi

v1_gh_hits=$(rg -n '"github\.com/charmbracelet/(bubbletea|bubbles|lipgloss|huh)(/|")' \
	--type go --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null |
	rg -v '// ' || true) # exclude comments
if [[ -n "$v1_gh_hits" ]]; then
	echo "FAIL [no-v1-charm-gh] v1 github.com/charmbracelet import:"
	echo "$v1_gh_hits"
	fail "no-v1-charm-gh"
fi

# ── Rule 2: No time.Sleep in test assertions ─────────────────────────────────
# time.Sleep in _test.go files is almost always wrong — use teatest.WaitFor,
# channels, or other synchronization. We allow time.Sleep inside httptest
# server handlers (simulating slow servers) by excluding lines inside
# http.HandlerFunc closures heuristically.

sleep_hits=$(rg -n 'time\.Sleep' --type go --glob '*_test.go' \
	--glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null || true)
if [[ -n "$sleep_hits" ]]; then
	# Filter out time.Sleep inside httptest handlers: look for lines where
	# the surrounding context (within ~10 lines above) contains HandlerFunc.
	# Use rg -C to get context, then post-filter. For simplicity, we do a
	# second pass: for each file with a hit, check if the Sleep is inside
	# a HandlerFunc block.
	real_hits=""
	while IFS= read -r line; do
		file=$(echo "$line" | cut -d: -f1)
		lineno=$(echo "$line" | cut -d: -f2)
		# Check 15 lines above for HandlerFunc — crude but effective
		context=$(sed -n "$((lineno > 15 ? lineno - 15 : 1)),${lineno}p" "$file" 2>/dev/null || true)
		if ! echo "$context" | rg -q 'HandlerFunc|httptest'; then
			real_hits+="$line"$'\n'
		fi
	done <<<"$sleep_hits"

	if [[ -n "${real_hits%$'\n'}" ]]; then
		echo "FAIL [no-sleep-in-tests] time.Sleep in test files (use teatest.WaitFor or sync primitives):"
		echo "${real_hits%$'\n'}"
		fail "no-sleep-in-tests"
	fi
fi

# ── Rule 3: No pocketbase/dbx in standalone packages ────────────────────────
# These packages compile into standalone binaries (dbtui, markdb, zot) or are
# reusable without pocketbase (board LocalCache). Importing dbx would bloat
# the binary and violate the documented exception.

standalone_pkgs=(
	"internal/tui/dbtui"
	"internal/zot/local"
	"internal/board"
)
for pkg in "${standalone_pkgs[@]}"; do
	dbx_hits=$(rg -n '"github\.com/pocketbase/dbx"' --type go "$pkg/" 2>/dev/null || true)
	if [[ -n "$dbx_hits" ]]; then
		echo "FAIL [no-dbx-standalone] pocketbase/dbx import in standalone package $pkg:"
		echo "$dbx_hits"
		fail "no-dbx-standalone"
	fi
done

# ── Rule 4: CLI flags must have Local: true ──────────────────────────────────
# urfave/cli v3 flags without Local: true leak to parent/child commands.
# CLAUDE.md mandates Local: true on every flag.
#
# Intentional exceptions (parent flags that propagate to subcommands) are
# suppressed with a "// lint:no-local" comment on the flag definition line
# or on the line immediately above it.
#
# Strategy: find single-line flag definitions (common pattern) missing Local,
# then find multi-line definitions missing Local before the closing brace.
# Both passes filter out lines annotated with lint:no-local.

# Single-line flags: &cli.XxxFlag{...} all on one line, no "Local:"
singleline_hits=$(rg -n 'cli\.(String|Bool|Int|Float|StringSlice|IntSlice)Flag\{.*\}' \
	--type go --glob '!*_test.go' --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null |
	rg -v 'Local:|lint:no-local' || true)
if [[ -n "$singleline_hits" ]]; then
	echo "FAIL [flag-local-required] CLI flag missing Local: true:"
	echo "$singleline_hits"
	fail "flag-local-required"
fi

# Multi-line flags: &cli.XxxFlag{\n ... } without Local anywhere in the block.
# Use rg multiline to find flag blocks, then check each for Local.
# This is harder — we use a Go-aware approach: find opening lines, then scan
# forward for Local: before the next closing }.
multiline_opens=$(rg -n 'cli\.(String|Bool|Int|Float|StringSlice|IntSlice)Flag\{$' \
	--type go --glob '!*_test.go' --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null || true)
if [[ -n "$multiline_opens" ]]; then
	while IFS= read -r line; do
		file=$(echo "$line" | cut -d: -f1)
		lineno=$(echo "$line" | cut -d: -f2)
		# Check for lint:no-local on the opening line or the line above it
		prev=$((lineno > 1 ? lineno - 1 : 1))
		suppress=$(sed -n "${prev},${lineno}p" "$file" 2>/dev/null || true)
		if echo "$suppress" | rg -q 'lint:no-local'; then
			continue
		fi
		# Scan forward up to 20 lines for closing brace or Local:
		block=$(sed -n "${lineno},$((lineno + 20))p" "$file" 2>/dev/null || true)
		# Extract up to and including the first line with only whitespace + },
		flag_block=$(echo "$block" | sed -n '1,/^[[:space:]]*},\{0,1\}$/p')
		if ! echo "$flag_block" | rg -q 'Local:'; then
			echo "FAIL [flag-local-required] CLI flag missing Local: true:"
			echo "  $line"
			fail "flag-local-required"
		fi
	done <<<"$multiline_opens"
fi

# ── Rule 5: No exec.Command in process-replacing files ──────────────────────
# Any file that calls syscall.Exec (process replacement) should NOT also use
# exec.Command — that would leave a zombie parent. exec.CommandContext is OK
# (used for probes/data-fetching, not replacement).
#
# Suppress with "// lint:allow-exec-command" on the same line if truly needed.

syscall_exec_files=$(rg -l 'syscall\.Exec\b' --type go \
	--glob '!*_test.go' --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null || true)
for f in $syscall_exec_files; do
	# Flag exec.Command( but not exec.CommandContext(
	bad_hits=$(rg -n 'exec\.Command[^C]' "$f" 2>/dev/null |
		rg -v 'lint:allow-exec-command' || true)
	if [[ -n "$bad_hits" ]]; then
		echo "FAIL [no-exec-command-in-replacer] exec.Command in process-replacing file $f (use syscall.Exec or exec.CommandContext):"
		echo "$bad_hits"
		fail "no-exec-command-in-replacer"
	fi
done

# ── Rule 6: No cloud.Client.Upload in board package ─────────────────────────
# cloud.Client.Upload auto-prepends {username}/ to keys, which is wrong for
# shared board R2 paths. Board code must use CloudAdapter methods exclusively.

upload_hits=$(rg -n '\.Upload\(' --type go internal/board/ 2>/dev/null || true)
if [[ -n "$upload_hits" ]]; then
	echo "FAIL [no-upload-in-board] .Upload() in internal/board/ (use CloudAdapter, not cloud.Client.Upload):"
	echo "$upload_hits"
	fail "no-upload-in-board"
fi

# ── Rule 7: DrainStdin after every tea.Program.Run() ────────────────────────
# Every bubbletea program that writes to a TTY should call ui.DrainStdin()
# after p.Run() to flush stale DECRQM responses. Standalone packages (dbtui,
# board) are excluded — they use alt-screen which resets the terminal, and
# must not import internal/uikit directly for DrainStdin.
# internal/uikit/ui_spinner.go is excluded because it calls DrainStdin internally.

drain_exempt=(
	"internal/uikit/"
	"internal/tui/dbtui/"
	"internal/tui/board/"
)

# Find Go files (non-test) that import bubbletea and call .Run()
bt_files=$(rg -l 'charm\.land/bubbletea' --type go --glob '!*_test.go' \
	--glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null || true)

for f in $bt_files; do
	# Skip exempt paths
	skip=false
	for exempt in "${drain_exempt[@]}"; do
		if [[ "$f" == *"$exempt"* ]]; then
			skip=true
			break
		fi
	done
	$skip && continue

	# Check if file calls p.Run() (tea.Program runner)
	if rg -q '\.Run\(\)' "$f" 2>/dev/null; then
		if ! rg -q 'DrainStdin' "$f" 2>/dev/null; then
			echo "FAIL [drain-stdin-after-run] $f calls tea.Program.Run() but never calls DrainStdin()"
			fail "drain-stdin-after-run"
		fi
	fi
done

# ── Rule 8: Standalone package import boundaries ────────────────────────────
# dbtui and markdb must not import any sciminds/cli/internal/* packages outside
# their own subtree. zot may import shared infra and dbtui, but not markdb.
#
# Shared leaf packages (pure-lipgloss utilities with no project-specific deps)
# are allowed — they exist specifically to be importable by standalone binaries.

standalone_allowed=(
	"github.com/sciminds/cli/internal/uikit"
)

isolated_pkgs=("internal/tui/dbtui")
for pkg in "${isolated_pkgs[@]}"; do
	# Find imports of internal/* that are NOT within the package's own subtree.
	own_import="github.com/sciminds/cli/${pkg}"
	infra_hits=$(rg -n '"github\.com/sciminds/cli/internal/' --type go "$pkg/" 2>/dev/null |
		rg -v "\"${own_import}" || true)
	# Filter out allowed shared leaf packages.
	for allowed in "${standalone_allowed[@]}"; do
		infra_hits=$(echo "$infra_hits" | rg -v "\"${allowed}\"" || true)
	done
	if [[ -n "$infra_hits" ]]; then
		echo "FAIL [standalone-boundary] standalone package $pkg imports outside its subtree:"
		echo "$infra_hits"
		fail "standalone-boundary"
	fi
done

# ── Rule 9: No legacy sort package — use slices/maps ───────────────────────
# Go 1.21+ provides slices.Sort, slices.SortFunc, slices.SortStableFunc,
# slices.BinarySearch, slices.IsSortedFunc, etc. The old sort.Strings,
# sort.Slice, etc. are more verbose and less type-safe.
#
# Banned:  sort.Strings, sort.Ints, sort.Float64s        → slices.Sort
#          sort.Slice                                     → slices.SortFunc
#          sort.SliceStable                               → slices.SortStableFunc
#          sort.SliceIsSorted                             → slices.IsSortedFunc
#          sort.Search                                    → slices.BinarySearch[Func]
#
# Suppress with "// lint:allow-sort" on the same line if truly needed.

sort_hits=$(rg -n 'sort\.(Strings|Ints|Float64s|Slice|SliceStable|SliceIsSorted|Search)\b' \
	--type go --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null |
	rg -v 'lint:allow-sort' || true)
if [[ -n "$sort_hits" ]]; then
	echo "FAIL [no-legacy-sort] use slices.Sort/SortFunc/SortStableFunc instead of sort.*:"
	echo "$sort_hits"
	fail "no-legacy-sort"
fi

# ── Rule 10: No append-clone — use slices.Clone or slices.Concat ───────────
# append([]T(nil), src...)  → slices.Clone(src)
# append([]T{}, src...)     → slices.Clone(src)
# append([]T{a,b}, src...)  → slices.Concat([]T{a,b}, src)
# All have stdlib replacements since Go 1.21+.
#
# Suppress with "// lint:allow-append-clone" on the same line if truly needed.

clone_hits=$(rg -n 'append\(\[\]\w+(\(nil\)|\{[^}]*\}), ' \
	--type go --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null |
	rg -v 'lint:allow-append-clone' || true)
if [[ -n "$clone_hits" ]]; then
	echo "FAIL [no-append-clone] use slices.Clone or slices.Concat instead of append([]T{}/nil, ...):"
	echo "$clone_hits"
	fail "no-append-clone"
fi

# ── Rule 11: No make+copy byte clone — use bytes.Clone ────────────────────
# cp := make([]byte, len(x)); copy(cp, x) is a hand-rolled byte clone.
# bytes.Clone(x) does the same in one call.
#
# Suppress with "// lint:allow-byte-clone" on the same line if truly needed.

byte_clone_hits=$(rg -nU 'make\(\[\]byte, len\([^)]+\)\)\n\s*copy\(' \
	--type go --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null |
	rg -v 'lint:allow-byte-clone' || true)
if [[ -n "$byte_clone_hits" ]]; then
	echo "FAIL [no-byte-clone] use bytes.Clone instead of make([]byte, len(x)) + copy:"
	echo "$byte_clone_hits"
	fail "no-byte-clone"
fi

# ── Rule 12: Every internal/ package must have a package-level doc comment ──
# pkgsite (just docs) renders the first // Package <name> comment block as
# the package overview. Without it the package page is blank and unhelpful.
# A package satisfies this rule if ANY .go file in the directory starts its
# package clause with a doc comment, or if a doc.go file exists.
#
# Suppress with a file named .lint-no-pkg-doc in the package directory.

pkg_doc_hits=""
while IFS= read -r dir; do
	# Only check directories that contain non-test .go files (skip asset dirs).
	has_go=false
	for f in "$dir"/*.go; do
		[[ -e "$f" ]] || continue
		[[ "$f" == *_test.go ]] && continue
		has_go=true
		break
	done
	$has_go || continue

	# Skip testdata directories (not real packages).
	[[ "$dir" == */testdata* ]] && continue

	# Skip if suppressed
	[[ -f "$dir/.lint-no-pkg-doc" ]] && continue

	# Check for doc.go
	[[ -f "$dir/doc.go" ]] && continue

	# Check for // Package <name> comment in any .go file (non-test).
	has_pkg_doc=false
	for f in "$dir"/*.go; do
		[[ "$f" == *_test.go ]] && continue
		if rg -q '^// Package ' "$f" 2>/dev/null; then
			has_pkg_doc=true
			break
		fi
	done

	if ! $has_pkg_doc; then
		pkg_doc_hits+="  $dir"$'\n'
	fi
done < <(fd -t d --min-depth 1 . internal/ | sort)

if [[ -n "${pkg_doc_hits%$'\n'}" ]]; then
	echo "FAIL [pkg-doc-required] packages missing // Package <name> doc comment:"
	echo "${pkg_doc_hits%$'\n'}"
	fail "pkg-doc-required"
fi

# ── Rule 13: Scriptability parity — every interactive prompt must be registered
# Every huh.New* prompt and tea.NewProgram/kit.Run TUI launch must have an entry
# in scripts/scriptable-inventory.yml. This catches new interactive prompts that
# lack a non-interactive bypass.
#
# Call-site discovery uses ast-grep (scripts/interactive-calls.yml) for
# structural matching. Inventory entries are matched by file + type + nth
# (occurrence index), NOT line numbers — so refactors don't cause drift.
#
# Exempt paths are hard-coded below (TUI-only apps, shared infra).
# Suppress a specific call site with "// lint:no-scriptable" on the same line.

inventory="scripts/scriptable-inventory.yml"
interactive_rules="scripts/interactive-calls.yml"

# Exempt paths — these never need inventory entries.
scriptable_exempt=(
	"internal/tui/dbtui/"
	"internal/tui/board/"
	"internal/mdview/"
	"internal/cmdutil/confirm.go"
	"internal/uikit/"
)

# ── Step 1: Discover call sites via ast-grep ────────────────────────────────
# Output: deduplicated JSON array of {id, file, line} sorted by file then line.
sg_json=$(sg scan -r "$interactive_rules" --json 2>/dev/null | \
	jaq '[.[] | {id: .ruleId, file, line: (.range.start.line + 1)}] | unique_by(.file, .id, .line) | sort_by(.file, .line)' 2>/dev/null)

# Filter out exempt paths and lint:no-scriptable suppressions.
sg_filtered="[]"
while IFS= read -r entry; do
	[[ -z "$entry" || "$entry" == "null" ]] && continue
	file=$(echo "$entry" | jaq -r '.file')
	line=$(echo "$entry" | jaq -r '.line')

	# Skip exempt paths
	skip=false
	for exempt in "${scriptable_exempt[@]}"; do
		if [[ "$file" == *"$exempt"* ]]; then
			skip=true
			break
		fi
	done
	$skip && continue

	# Skip lint:no-scriptable suppressions
	if sed -n "${line}p" "$file" 2>/dev/null | rg -q 'lint:no-scriptable'; then
		continue
	fi

	sg_filtered=$(echo "$sg_filtered" | jaq --argjson e "$entry" '. + [$e]')
done < <(echo "$sg_json" | jaq -c '.[]')

# ── Step 2: Assign nth (occurrence index) per file+type ─────────────────────
# For each unique (file, type) pair, number occurrences 1, 2, 3… by line order.
sg_with_nth=$(echo "$sg_filtered" | jaq '
	group_by(.file, .id)
	| [.[] | to_entries[] | .value + {nth: (.key + 1)}]
	| sort_by(.file, .line)')

# ── Step 3: Build inventory lookup from YAML ────────────────────────────────
# Extract file + type + nth triples from the inventory.
inv_entries=""
if [[ -f "$inventory" ]]; then
	current_file=""
	while IFS= read -r line; do
		if [[ "$line" =~ ^[[:space:]]*file:[[:space:]]*(.+)$ ]]; then
			current_file="${BASH_REMATCH[1]}"
		fi
		if [[ "$line" =~ ^[[:space:]]*type:[[:space:]]*(.+)$ ]]; then
			current_type="${BASH_REMATCH[1]}"
		fi
		if [[ "$line" =~ ^[[:space:]]*nth:[[:space:]]*([0-9]+) ]]; then
			inv_entries+="${current_file}|${current_type}|${BASH_REMATCH[1]}"$'\n'
		fi
	done < "$inventory"
fi

# ── Step 4: Check every discovered call site is registered ──────────────────
while IFS= read -r entry; do
	[[ -z "$entry" || "$entry" == "null" ]] && continue
	file=$(echo "$entry" | jaq -r '.file')
	id=$(echo "$entry" | jaq -r '.id')
	nth=$(echo "$entry" | jaq -r '.nth')
	line=$(echo "$entry" | jaq -r '.line')

	# Convert ast-grep id (huh-NewInput) to inventory type (huh.NewInput)
	inv_type=$(echo "$id" | sed 's/-/./')

	lookup="${file}|${inv_type}|${nth}"
	if ! echo "$inv_entries" | rg -qF "$lookup"; then
		echo "FAIL [scriptable-parity] unregistered interactive prompt: $file:$line ($inv_type, nth=$nth)"
		echo "  Add an entry to $inventory or suppress with // lint:no-scriptable"
		fail "scriptable-parity"
	fi
done < <(echo "$sg_with_nth" | jaq -c '.[]')

# ── Step 5: Detect stale inventory entries ──────────────────────────────────
# For each inventory entry (file+type+nth), check that ast-grep still finds it.
if [[ -f "$inventory" ]]; then
	current_file=""
	current_type=""
	stale_warnings=""
	while IFS= read -r line; do
		if [[ "$line" =~ ^[[:space:]]*file:[[:space:]]*(.+)$ ]]; then
			current_file="${BASH_REMATCH[1]}"
		fi
		if [[ "$line" =~ ^[[:space:]]*type:[[:space:]]*(.+)$ ]]; then
			current_type="${BASH_REMATCH[1]}"
		fi
		if [[ "$line" =~ ^[[:space:]]*nth:[[:space:]]*([0-9]+) ]]; then
			inv_nth="${BASH_REMATCH[1]}"
			# Convert inventory type (huh.NewInput) to ast-grep id (huh-NewInput)
			sg_id=$(echo "$current_type" | sed 's/\./-/')
			# Check if ast-grep found this file+type+nth
			found=$(echo "$sg_with_nth" | jaq --arg f "$current_file" --arg id "$sg_id" --argjson n "$inv_nth" \
				'[.[] | select(.file == $f and .id == $id and .nth == $n)] | length')
			if [[ "$found" == "0" ]]; then
				if [[ ! -f "$current_file" ]]; then
					stale_warnings+="  WARN [scriptable-parity] stale entry: $current_file — file not found"$'\n'
				else
					stale_warnings+="  WARN [scriptable-parity] stale entry: $current_file ($current_type nth=$inv_nth) — no matching call"$'\n'
				fi
			fi
		fi
	done < "$inventory"

	if [[ -n "${stale_warnings%$'\n'}" ]]; then
		echo "${stale_warnings%$'\n'}"
		# Stale entries are warnings, not errors — they don't block the build.
	fi
fi

# Fail on any inventory entries with status: gap — these are tracked debt that
# must be resolved (add a non-interactive bypass, then flip to "covered").
if [[ -f "$inventory" ]]; then
	current_cmd=""
	current_id=""
	gap_hits=""
	while IFS= read -r line; do
		if [[ "$line" =~ ^[[:space:]]{2}[a-z] ]] && [[ "$line" =~ ^[[:space:]]*([a-z][-a-z]*): ]]; then
			current_cmd="${BASH_REMATCH[1]}"
		fi
		if [[ "$line" =~ ^[[:space:]]*-[[:space:]]*id:[[:space:]]*(.+)$ ]]; then
			current_id="${BASH_REMATCH[1]}"
		fi
		if [[ "$line" =~ ^[[:space:]]*status:[[:space:]]*gap ]]; then
			gap_hits+="  $current_cmd/$current_id"$'\n'
		fi
	done < "$inventory"

	if [[ -n "${gap_hits%$'\n'}" ]]; then
		echo "FAIL [scriptable-gap] inventory entries with status: gap (missing non-interactive bypass):"
		echo "${gap_hits%$'\n'}"
		fail "scriptable-gap"
	fi
fi

# ── Rule 14: No raw huh .Run() — use uikit.RunForm ──────────────────────────
# huh forms run bubbletea internally. After .Run(), stdin must be drained to
# absorb stale DECRQM responses. uikit.RunForm handles theme, keymap, and
# drain in one call. Files outside internal/uikit/ and internal/cmdutil/ that
# import huh must not call .Run() directly.

huh_raw_exempt=(
	"internal/uikit/"
	"internal/cmdutil/"
)

huh_files=$(rg -l 'charm\.land/huh/v2' --type go --glob '!*_test.go' \
	--glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null || true)

for f in $huh_files; do
	skip=false
	for exempt in "${huh_raw_exempt[@]}"; do
		if [[ "$f" == *"$exempt"* ]]; then
			skip=true
			break
		fi
	done
	$skip && continue

	# Check for huh.NewForm(...).Run() or form.Run() patterns — but not
	# exec.Command(...).Run() or other unrelated .Run() calls.
	# We look for huh.NewForm in the file AND a .Run() call on a line that
	# contains huh or form (case-insensitive) but not exec.Command.
	if rg -q 'huh\.NewForm' "$f" 2>/dev/null; then
		raw_run=$(rg -n '\.Run\(\)' "$f" 2>/dev/null | rg -iv 'exec\.Command|cmd\.Run|\.Start' | rg -i 'form\|huh\|WithTheme\|WithKeyMap' || true)
		if [[ -n "$raw_run" ]]; then
			echo "FAIL [no-raw-huh-run] $f calls .Run() on a huh form directly — use uikit.RunForm():"
			echo "$raw_run"
			fail "no-raw-huh-run"
		fi
	fi
done

# ── Summary ──────────────────────────────────────────────────────────────────
if [[ $errors -gt 0 ]]; then
	echo ""
	echo "lint-guard: ${#failed_rules[@]} rule(s) failed: ${failed_rules[*]}"
	exit 1
fi
