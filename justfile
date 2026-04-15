commit := `git rev-parse --short HEAD 2>/dev/null || echo unknown`
ldflags := "-s -w -X github.com/sciminds/cli/internal/version.Commit=" + commit

build:
    go build -ldflags="{{ldflags}}" -o sci ./cmd/sci
    go build -ldflags="-s -w" -o dbtui ./cmd/dbtui
    go build -ldflags="-s -w" -o zot ./cmd/zot

tidy:
    go mod tidy

fmt:
    gofmt -w .
    goimports -w .

lint:
    golangci-lint run ./internal/... ./cmd/...

# Structural style rules enforced via ast-grep.
# No lipgloss.NewStyle() outside ui/ packages; no hardcoded lipgloss.Color() outside palette/style files.
lint-style:
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

check: tidy fmt vet lint lint-style lint-guard test build

ok: check
    @echo "All checks passed."

# Like `ok` but skips lint-style (semgrep/ast-grep). Use when semgrep
# rules are temporarily broken or being updated.
ok-legacy: tidy fmt vet lint lint-guard test build
    @echo "All checks (no semgrep) passed."

# Like `ok` but also runs proj/new integration tests (SLOW=1).
# Requires pixi, uv, quarto, marimo, typst, node on PATH.
# Does NOT run test-canvas / test-zot-real — those need
# credentials or live infra and stay opt-in.
ok-slow: tidy fmt vet lint lint-style lint-guard test test-slow build
    @echo "All checks (incl. slow) passed."

clean:
    rm -f sci dbtui zot

# Render embedded asciicasts to GIFs under docs/casts/ using `agg`.
# GIFs embed natively in GitHub-rendered markdown (no JS player needed).
# Pass a filename stem (or glob) to limit the set:
#     just casts-gif              # all casts
#     just casts-gif zot-doctor   # just one
#     just casts-gif 'zot-*'      # all zot casts (quote to protect from shell)
casts-gif FILTER='*':
    #!/usr/bin/env bash
    set -euo pipefail
    command -v agg >/dev/null || { echo "agg not found on PATH — install from https://github.com/asciinema/agg"; exit 1; }
    mkdir -p docs/casts
    shopt -s nullglob
    casts=(internal/guide/casts/{{FILTER}}.cast)
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
# Not part of `check`/`ok` gate — run manually or in doc-audit sessions.
lint-docs:
    golangci-lint run --config .golangci-docs.yml ./internal/...

# Report gaps in user-facing CLI documentation (casts, gifs, README embeds, help descriptions).
doc-coverage:
    ./scripts/doc-coverage.sh

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

