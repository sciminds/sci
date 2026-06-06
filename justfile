commit := `git rev-parse --short HEAD 2>/dev/null || echo unknown`
ldflags := "-s -w -X github.com/sciminds/cli/internal/version.Commit=" + commit

build:
    go build -ldflags="{{ldflags}}" -o sci ./cmd/sci

tidy:
    go mod tidy

# Install Go dev tools pinned in go.mod's `tool` block (goimports,
# golangci-lint, gopls, gofumpt, dlv) into $GOBIN. Run once after cloning.
bootstrap:
    go install tool

fmt:
    gofmt -w .
    goimports -w .

lint:
    golangci-lint run ./internal/... ./cmd/...

# Structural style rules enforced via ast-grep.
# No lipgloss.NewStyle() outside ui/ packages; no hardcoded lipgloss.Color() outside palette/style files;
# no manual m.width/m.height literal arithmetic outside uikit. `sg test` validates the rules' own fixtures.
lint-style:
    sg test
    sg scan
    semgrep --config .semgrep/ --error --quiet ./internal/ ./cmd/

# Project-specific guards: import boundaries, flag conventions, API usage rules.
lint-guard:
    ./scripts/lint-guard.sh

# Check interactive ↔ non-interactive parity (alias for lint-guard).
scriptable: lint-guard

vet:
    go vet ./internal/... ./cmd/...

test:
    go test ./... -count=1

# Race-detector pass. Slower than `test` so it lives on the pre-commit gate
# (`just ok`) rather than the fast TDD loop (`just test` / `just test-pkg`).
test-race:
    go test ./... -race -count=1

# Run tests for a single package (fast TDD iteration). `just test-pkg ./internal/zot`
test-pkg PKG *ARGS:
    go test {{PKG}} -count=1 {{ARGS}}

# Integration tests needing external tools (pixi, uv, quarto, marimo, typst, node)
test-slow *ARGS:
    SLOW=1 go test ./internal/proj/new -v -timeout 10m -count=1 {{ARGS}}

# Canvas + GitHub Classroom integration tests (requires CANVAS_TOKEN in .env and gh auth)
test-canvas:
    SLOW=1 CANVAS_TEST_TOKEN=$CANVAS_TOKEN CANVAS_TEST_URL="https://canvas.ucsd.edu/courses/63653" GH_CLASSROOM_TEST_URL="https://classroom.github.com/classrooms/232475786-test-classroom" go test ./internal/cass/ -run Integration -v -timeout 2m -count=1

# Real-Zotero-DB smoke (opt-in; reads ./zotero.sqlite or $ZOT_REAL_DB)
test-zot-real:
    SLOW=1 go test ./internal/zot/local/ ./internal/zot/hygiene/ -v -count=1

test-all: test test-slow

check: tidy fmt vet lint lint-style lint-docs lint-guard test test-race build

# CI gate — verify-only (no file writes), no multi-arch build, no lint-style.
# Mirrors `check` so the local and CI gates can't drift: add a step here and
# .github/workflows/release.yml picks it up. Skips lint-style (semgrep +
# ast-grep) to avoid the runner install; keeps lint-guard (pure bash + rg).
check-ci:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "==> tidy (verify go.mod / go.sum committed)"
    go mod tidy
    git diff --exit-code go.mod go.sum
    echo "==> fmt (verify gofmt + goimports clean)"
    out=$(gofmt -l .); if [ -n "$out" ]; then echo "gofmt drift:"; echo "$out"; exit 1; fi
    out=$(goimports -l .); if [ -n "$out" ]; then echo "goimports drift:"; echo "$out"; exit 1; fi
    echo "==> vet"
    go vet ./internal/... ./cmd/...
    echo "==> lint"
    golangci-lint run ./internal/... ./cmd/...
    echo "==> lint-docs"
    golangci-lint run --config .golangci-docs.yml ./internal/...
    echo "==> lint-guard"
    ./scripts/lint-guard.sh
    echo "==> test -race"
    go test ./... -race -count=1

ok: check
    @echo "All checks passed."

# Like `ok` but skips lint-style (semgrep/ast-grep). Use when semgrep
# rules are temporarily broken or being updated.
ok-legacy: tidy fmt vet lint lint-docs lint-guard test build
    @echo "All checks (no semgrep) passed."

# Like `ok` but also runs proj/new integration tests (SLOW=1).
# Requires pixi, uv, quarto, marimo, typst, node on PATH.
# Does NOT run test-canvas / test-zot-real — those need
# credentials or live infra and stay opt-in.
ok-slow: tidy fmt vet lint lint-style lint-docs lint-guard test test-slow build
    @echo "All checks (incl. slow) passed."

clean:
    rm -f sci

# Regenerate internal/uikit/REFERENCE.md from godoc comments.
docs-uikit:
    go run ./internal/uikit/cmd/gen-reference ./internal/uikit ./internal/uikit/REFERENCE.md

# Render embedded asciicasts to GIFs under docs/casts/ using `agg`.
# Searches both internal/help/casts/ (sci command demos) and
# internal/learn/casts/ (general terminal/git/python tutorials).
# Pass a filename stem (or glob) to limit the set:
#     just casts-gif              # all casts in both dirs
#     just casts-gif zot-doctor   # just one
#     just casts-gif 'zot-*'      # all zot casts (quote to protect from shell)
casts-gif FILTER='*':
    #!/usr/bin/env bash
    set -euo pipefail
    command -v agg >/dev/null || { echo "agg not found on PATH — install from https://github.com/asciinema/agg"; exit 1; }
    mkdir -p docs/casts
    shopt -s nullglob
    casts=()
    for glob in internal/help/casts/{{FILTER}}.cast internal/learn/casts/{{FILTER}}.cast; do
        [[ -f "$glob" ]] && casts+=("$glob")
    done
    if [[ ${#casts[@]} -eq 0 ]]; then
        # Fall back to shell glob expansion for wildcard filters.
        for c in internal/help/casts/{{FILTER}}.cast internal/learn/casts/{{FILTER}}.cast; do
            casts+=("$c")
        done
    fi
    if [[ ${#casts[@]} -eq 0 ]]; then
        echo "no casts matched '{{FILTER}}'"
        exit 1
    fi
    for cast in "${casts[@]}"; do
        name=$(basename "$cast" .cast)
        out="docs/casts/$name.gif"
        echo "  → $out"
        agg --theme github-dark --font-size 14 --speed 1.2 --idle-time-limit 1 "$cast" "$out"
    done
    echo "rendered ${#casts[@]} gif(s) to docs/casts/"

# Check Go doc comments (package-level + exported symbols) via revive.
# Part of the `check`/`ok` gate; also runnable standalone for doc-audit sessions.
lint-docs:
    golangci-lint run --config .golangci-docs.yml ./internal/...

# Report gaps in user-facing CLI documentation (casts, gifs, README embeds, help descriptions).
doc-coverage:
    ./scripts/doc-coverage.sh

# ─── Modernization via `go fix` (Go 1.26) ──────────────────────────────────
# Go 1.26 rewrote `go fix` into a suite of modernizers (the same ones gopls
# runs). We apply them in reviewable stages and commit each separately. Append
# `-diff` to any recipe to preview without writing, e.g. `just modernize-safe -diff`.
# Not part of the `ok` gate — run manually when bumping the Go toolchain.
# Excluded everywhere: omitzero (possible behavior change — handle by hand).
# Fixes touching generated files (e.g. zotero.gen.go) are skipped automatically;
# modernize those via the generator/template, not the file.

# Preview every auto-appliable safe fix (stages 1+2), writes nothing.
modernize-diff *ARGS:
    go fix -diff -omitzero=false -newexpr=false {{ARGS}} ./...

# Stage 1 — replace interface{} with `any` in hand-written files. Optionally
# commit alone; here it's a single site so it folds into `modernize-safe`.
modernize-any *ARGS:
    go fix -any {{ARGS}} ./...

# Stage 2 — remaining safe modernizers: minmax, rangeint, slices/strings
# helpers, forvar, waitgroup, … (excludes any, omitzero, and newexpr).
# Re-run until `just modernize-diff` is empty: synergistic fixes (e.g. an
# if-clamp left beside a fresh min() becomes a nested max(min(…))) surface
# only on a second pass. Pass a single analyzer to scope, e.g. `-minmax`.
modernize-safe *ARGS:
    go fix -any=false -omitzero=false -newexpr=false {{ARGS}} ./...

# Stage 3 — newexpr: rewrite ptr(x) helpers + calls to Go 1.26's new(x).
# Leaves the now-unused helper funcs as dead code; delete them by hand, then
# `just ok`. Preview first with `just modernize-deadcode -diff`.
modernize-deadcode *ARGS:
    go fix -newexpr {{ARGS}} ./...

run *ARGS:
    go run ./cmd/sci {{ARGS}}

zot-spec := "/Users/esh/Documents/webapps/apis/zotero/openapi.yaml"

# Regenerate the Zotero API Go client from the OpenAPI spec.
# Downgrades 3.1 -> 3.0 on the fly (type:[T,null] -> nullable:true) because
# oapi-codegen v2 does not yet support 3.1 union types.
zot-gen:
    @tmp=$(mktemp -t zotero-spec.XXXXXX.yaml); \
    sd '^openapi: 3\.1\.0$' 'openapi: 3.0.3' < {{zot-spec}} \
        | sd 'type: \[string, "null"\]' 'type: string\n          nullable: true' \
        | sd 'type: \[integer, "null"\]' 'type: integer\n          nullable: true' \
        | sd '^    tag:$' '    tagFilter:' \
        | sd '#/components/parameters/tag"' '#/components/parameters/tagFilter"' \
        | yq eval-all "$(cat scripts/zotero-mirror-paths.yq)" - \
        > $tmp; \
    (cd internal/zot/client && oapi-codegen -config config.yaml $tmp); \
    rm -f $tmp
    gofmt -w internal/zot/client/zotero.gen.go

# Open package documentation in the browser
docs:
    @echo "Starting pkgsite at http://localhost:6060/github.com/sciminds/cli"
    open "http://localhost:6060/github.com/sciminds/cli"
    pkgsite -http=localhost:6060

set dotenv-load

# Deploy Cloudflare Worker (sci-auth)
worker-deploy:
    cd worker && npx wrangler deploy

