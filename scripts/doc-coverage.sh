#!/usr/bin/env bash
# doc-coverage.sh — report gaps in user-facing CLI documentation.
#
# Cross-references four surfaces for each top-level sci command group:
#   1. help long description     (internal/help/descs.go)
#   2. Asciinema cast file       (internal/help/casts/<cmd>.cast or <cmd>-<sub>.cast)
#   3. Rendered GIF              (docs/casts/<cmd>.gif or docs/casts/<cmd>-<sub>.gif)
#   4. README.md embed           (![...](docs/casts/<cmd>.gif))
#
# Those four are the gate: a missing one is a "gap" and exits 1. The final
# cast-without-GIF sweep is a warning only — see the comment on that block.
#
# Run via `just doc-coverage`.
set -euo pipefail

ROOT=$(git rev-parse --show-toplevel 2>/dev/null || pwd)

CASTS_DIR="$ROOT/internal/help/casts"
GIFS_DIR="$ROOT/docs/casts"
DESCS_FILE="$ROOT/internal/help/descs.go"
README="$ROOT/README.md"

# ── Discover top-level command groups from cmd/sci/root.go ───────────────────
# root.go registers commands as xxxCommand() calls. We extract the function
# prefix to get the command name (e.g. cloudCommand → cloud).

commands=()
while IFS= read -r name; do
    commands+=("$name")
done < <(rg '^\s+(\w+)Command\(\),' -or '$1' "$ROOT/cmd/sci/root.go" 2>/dev/null | sort -u)

# ── Check functions ──────────────────────────────────────────────────────────

check_desc() {
    local cmd="$1"
    rg -q "\"$cmd\":" "$DESCS_FILE" 2>/dev/null
}

check_cast() {
    # Covered if a leaf cast ($cmd.cast, e.g. doctor.cast) or any
    # per-subcommand cast ($cmd-<sub>.cast, e.g. proj-new.cast) exists.
    local cmd="$1"
    [[ -f "$CASTS_DIR/$cmd.cast" ]] && return 0
    compgen -G "$CASTS_DIR/$cmd-*.cast" > /dev/null
}

check_gif() {
    local cmd="$1"
    [[ -f "$GIFS_DIR/$cmd.gif" ]] && return 0
    compgen -G "$GIFS_DIR/$cmd-*.gif" > /dev/null
}

check_readme() {
    # README is covered if it embeds any gif whose name starts with $cmd
    # (either $cmd.gif or $cmd-<sub>.gif).
    local cmd="$1"
    rg -q "docs/casts/$cmd(-[^)]*)?\.gif" "$README" 2>/dev/null
}

# ── Report ───────────────────────────────────────────────────────────────────

pass="\033[32mPASS\033[0m"
miss="\033[31mMISS\033[0m"

printf "\n%-14s %-10s %-10s %-10s %-10s\n" "COMMAND" "HELP-DESC" "CAST" "GIF" "README"
printf "%-14s %-10s %-10s %-10s %-10s\n" "-------" "---------" "----" "---" "------"

gaps=0

for cmd in "${commands[@]}"; do
    # Skip meta-commands that don't need cast/gif coverage.
    # "help" is the interactive help browser itself — it doesn't need a demo of
    # itself. "version" prints one line, so a recorded terminal session would
    # show strictly less than the README table row already does.
    [[ "$cmd" == "help" || "$cmd" == "version" ]] && continue

    d_status="$pass"; c_status="$pass"; g_status="$pass"; r_status="$pass"

    if ! check_desc "$cmd"; then d_status="$miss"; ((gaps++)) || true; fi
    if ! check_cast "$cmd"; then c_status="$miss"; ((gaps++)) || true; fi
    if ! check_gif  "$cmd"; then g_status="$miss"; ((gaps++)) || true; fi
    if ! check_readme "$cmd"; then r_status="$miss"; ((gaps++)) || true; fi

    printf "%-14s %-10b %-10b %-10b %-10b\n" "sci $cmd" "$d_status" "$c_status" "$g_status" "$r_status"
done

# ── Zot subcommand casts (named zot-<sub>.cast, not sci-zot-<sub>.cast) ─────

printf "\n%-14s %-10s %-10s %-10s %-10s\n" "ZOT CASTS" "" "CAST" "GIF" "README"
printf "%-14s %-10s %-10s %-10s %-10s\n" "---------" "" "----" "---" "------"

# Discover zot cast stems from existing casts
zot_subs=()
while IFS= read -r f; do
    stem=$(basename "$f" .cast)
    zot_subs+=("$stem")
done < <(fd -e cast 'zot-' "$CASTS_DIR" 2>/dev/null | sort)

for stem in "${zot_subs[@]}"; do
    c_status="$pass"; g_status="$pass"; r_status="$pass"

    [[ -f "$GIFS_DIR/$stem.gif" ]] || { g_status="$miss"; ((gaps++)) || true; }
    rg -q "docs/casts/$stem\.gif" "$README" 2>/dev/null || { r_status="$miss"; ((gaps++)) || true; }

    printf "%-14s %-10s %-10b %-10b %-10b\n" "$stem" "" "$c_status" "$g_status" "$r_status"
done

# ── Orphan check: GIFs without README embed ─────────────────────────────────

orphan_gifs=""
for gif in "$GIFS_DIR"/*.gif; do
    [[ -f "$gif" ]] || continue
    name=$(basename "$gif")
    if ! rg -q "docs/casts/$name" "$README" 2>/dev/null; then
        orphan_gifs+="  $name"$'\n'
    fi
done

if [[ -n "${orphan_gifs%$'\n'}" ]]; then
    printf "\n\033[33mOrphan GIFs\033[0m (in docs/casts/ but not referenced in README):\n"
    echo "${orphan_gifs%$'\n'}"
    ((gaps++)) || true
fi

# ── Cast without GIF check ──────────────────────────────────────────────────
#
# Casts are named <cmd>.cast / <cmd>-<sub>.cast (there is no `sci-` prefix),
# and a rendered GIF shares the stem: docs/casts/<stem>.gif.
#
# This sweep is a WARNING, not a gate failure. Every cast is recorded by hand
# and rendered separately via `just casts-gif`, so the repo carries a standing
# backlog of casts whose GIF has not been rendered yet. Failing the gate on
# that backlog would make `just doc-coverage` permanently red for a condition
# nobody is about to fix in the commit that tripped it, which trains people to
# ignore the whole report. The four per-command columns above stay hard.

unrendered_casts=""
unrendered_count=0
for cast in "$CASTS_DIR"/*.cast; do
    [[ -f "$cast" ]] || continue
    stem=$(basename "$cast" .cast)
    if [[ ! -f "$GIFS_DIR/$stem.gif" ]]; then
        unrendered_casts+="  $stem.cast (no $stem.gif)"$'\n'
        ((unrendered_count++)) || true
    fi
done

if [[ $unrendered_count -gt 0 ]]; then
    printf "\n\033[33mCasts without GIFs\033[0m (%d; warning only — run \`just casts-gif\` to render):\n" \
        "$unrendered_count"
    echo "${unrendered_casts%$'\n'}"
fi

# ── Summary ──────────────────────────────────────────────────────────────────

echo ""
if [[ $gaps -gt 0 ]]; then
    printf "\033[31m%d gap(s) found.\033[0m Run \`just doc-coverage\` after fixes to verify.\n" "$gaps"
    exit 1
elif [[ $unrendered_count -gt 0 ]]; then
    printf "\033[32mEvery command has a help description, a cast, a GIF and a README embed.\033[0m\n"
    printf "\033[33m%d cast(s) still have no rendered GIF\033[0m — run \`just casts-gif\` to close the backlog.\n" \
        "$unrendered_count"
else
    printf "\033[32mAll commands fully documented.\033[0m\n"
fi
