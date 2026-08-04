#!/usr/bin/env bash
# lint-commits.sh — enforce the commit-subject convention (Conventional Commits).
#
#   <type>(<scope>)!: <subject>
#
# types: feat fix docs refactor perf test chore ci build style
# scope: optional, lowercase package/area (zot, db, uikit, zot/local, …)
# `!` before the colon marks a breaking change.
# Merge and revert commits are exempt.
#
# Release notes are generated from these subjects by git-cliff (cliff.toml):
# feat/fix/perf/refactor/docs are published, the rest are skipped — so the
# type a commit carries decides whether users ever see it.
#
# Modes:
#   lint-commits.sh --message <file>   validate one commit-message file
#                                      (called by the scripts/commit-msg hook)
#   lint-commits.sh [<range>]          validate every subject in a rev range
#                                      (default: origin/main..HEAD; CI passes
#                                      the pushed/PR range explicitly)

set -euo pipefail

PATTERN='^(feat|fix|docs|refactor|perf|test|chore|ci|build|style)(\([a-z0-9./,-]+\))?!?: .+'
EXEMPT='^(Merge |Revert )'

fail_msg() {
    cat >&2 <<'EOF'
Commit subject does not match the convention: <type>(<scope>)!: <subject>
  types:  feat fix docs refactor perf test chore ci build style
  scope:  optional, lowercase (package or area: zot, db, uikit, ...)
  !:      breaking change marker, before the colon

Examples:
  feat(zot): browse — inline search-and-open REPL
  fix(db): duckdb subprocesses run -no-init
  feat(zot)!: extract-lib under --json requires --plan, --yes, or --apply
  docs: fact-checked sweep of CLAUDE.md

See CLAUDE.md "Commit convention".
EOF
}

check_subject() {
    local subject=$1
    [[ $subject =~ $EXEMPT ]] && return 0
    [[ $subject =~ $PATTERN ]]
}

if [[ ${1:-} == "--message" ]]; then
    [[ $# -eq 2 ]] || { echo "usage: lint-commits.sh --message <file>" >&2; exit 2; }
    subject=$(head -n1 "$2")
    if ! check_subject "$subject"; then
        echo "✗ ${subject}" >&2
        echo >&2
        fail_msg
        exit 1
    fi
    exit 0
fi

range=${1:-origin/main..HEAD}
bad=0
while IFS= read -r line; do
    [[ -z $line ]] && continue
    sha=${line%% *}
    subject=${line#* }
    if ! check_subject "$subject"; then
        echo "✗ ${sha} ${subject}" >&2
        bad=1
    fi
done < <(git log --no-merges --format='%h %s' "$range")

if (( bad )); then
    echo >&2
    fail_msg
    exit 1
fi
echo "commit subjects OK (${range})"
