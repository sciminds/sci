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
  --type go --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null \
  | rg -v '// ' || true)  # exclude comments
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
  done <<< "$sleep_hits"

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
  "internal/markdb"
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
  --type go --glob '!*_test.go' --glob '!.agents/**' --glob '!vendor/**' . 2>/dev/null \
  | rg -v 'Local:|lint:no-local' || true)
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
  done <<< "$multiline_opens"
fi

# ── Rule 5: No exec.Command in process-replacing packages ───────────────────
# internal/py/ephemeral.go must use syscall.Exec for process replacement.
# exec.Command would leave a zombie parent. Other files in internal/py/
# (e.g. convert.go) may legitimately use exec.Command.

if [[ -f internal/py/ephemeral.go ]]; then
  exec_cmd_hits=$(rg -n 'exec\.Command' internal/py/ephemeral.go 2>/dev/null || true)
  if [[ -n "$exec_cmd_hits" ]]; then
    echo "FAIL [no-exec-command-ephemeral] exec.Command in ephemeral.go (use syscall.Exec):"
    echo "$exec_cmd_hits"
    fail "no-exec-command-ephemeral"
  fi
fi

# ── Rule 6: No cloud.Client.Upload in board package ─────────────────────────
# cloud.Client.Upload auto-prepends {username}/ to keys, which is wrong for
# shared board R2 paths. Board code must use CloudAdapter methods exclusively.

upload_hits=$(rg -n '\.Upload\(' --type go internal/board/ 2>/dev/null || true)
if [[ -n "$upload_hits" ]]; then
  echo "FAIL [no-upload-in-board] .Upload() in internal/board/ (use CloudAdapter, not cloud.Client.Upload):"
  echo "$upload_hits"
  fail "no-upload-in-board"
fi

# ── Summary ──────────────────────────────────────────────────────────────────
if [[ $errors -gt 0 ]]; then
  echo ""
  echo "lint-guard: ${#failed_rules[@]} rule(s) failed: ${failed_rules[*]}"
  exit 1
fi
