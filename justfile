commit := `git rev-parse --short HEAD 2>/dev/null || echo unknown`
ldflags := "-s -w -X github.com/sciminds/cli/internal/version.Commit=" + commit

build:
    go build -ldflags="{{ldflags}}" -o sci ./cmd/sci
    go build -ldflags="-s -w" -o dbtui ./cmd/dbtui
    go build -ldflags="-s -w" -o markdb ./cmd/markdb
    go build -ldflags="-s -w" -o zot ./cmd/zot
    go build -ldflags="-s -w" -o boarddemo ./cmd/boarddemo

tidy:
    go mod tidy

fmt:
    gofmt -w .
    goimports -w .

lint:
    golangci-lint run ./internal/... ./cmd/...

vet:
    go vet ./internal/... ./cmd/...

test:
    go test ./... -count=1

# Integration tests needing external tools (pixi, uv, quarto, marimo, typst, node)
test-slow *ARGS:
    SLOW=1 go test ./internal/proj/new -v -timeout 10m -count=1 {{ARGS}}

# Canvas + GitHub Classroom integration tests (requires CANVAS_TOKEN in .env and gh auth)
test-canvas:
    CANVAS_TEST_TOKEN=$CANVAS_TOKEN CANVAS_TEST_URL="https://canvas.ucsd.edu/courses/63653" GH_CLASSROOM_TEST_URL="https://classroom.github.com/classrooms/232475786-test-classroom" go test ./internal/cass/ -run Integration -v -timeout 2m -count=1

test-all: test test-slow

check: tidy fmt vet lint test build

ok: check
    @echo "All checks passed."

clean:
    rm -f sci dbtui markdb zot boarddemo

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

# Deploy PocketBase hooks to PocketHost via FTP
pb-deploy:
    curl -u "$GOOSE_CLOUD_SUPERUSER_EMAIL:$GOOSE_CLOUD_SUPERUSER_PASS" --ftp-create-dirs \
        -T pocketbase/pb_hooks/org_guard.pb.js \
        ftp://ftp.pockethost.io/goose/pb_hooks/org_guard.pb.js
    @echo "Deployed pb_hooks to PocketHost."

# Deploy Cloudflare Worker (sci-auth)
worker-deploy:
    cd worker && npx wrangler deploy

# List deployed PocketBase hooks on PocketHost
pb-status:
    curl -u "$GOOSE_CLOUD_SUPERUSER_EMAIL:$GOOSE_CLOUD_SUPERUSER_PASS" --list-only \
        ftp://ftp.pockethost.io/goose/pb_hooks/
