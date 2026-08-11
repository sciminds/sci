# shellcheck shell=bash
# pipeline-lib.sh — the decision functions of the zot pipeline runner.
#
# Everything here is PURE: it reads files and prints a verdict. Nothing in
# this file runs a stage, spends money, or writes state. That separation is
# what makes the two gates that matter — the WAL gate and the OpenAlex
# leash — testable against fixtures instead of against a live library.
#
# Sourced by ./zot-pipeline (the runner) and by ./test-pipeline.sh (the
# tests). It must stay side-effect free at source time.

# ---------------------------------------------------------------------------
# JSON helpers
# ---------------------------------------------------------------------------

# pl_json <file> <jq-filter> [default]
# Prints the filter's result, or `default` when the file is missing, the
# JSON is unparseable, or the value is null. A missing number must not
# collapse into an empty string that later arithmetic reads as zero.
pl_json() {
    local file=$1 filter=$2 default=${3:-}
    local out
    if [[ ! -r $file ]]; then
        printf '%s\n' "$default"
        return 0
    fi
    if ! out=$(jq -er "$filter" <"$file" 2>/dev/null); then
        printf '%s\n' "$default"
        return 0
    fi
    printf '%s\n' "$out"
}

# ---------------------------------------------------------------------------
# The WAL gate
# ---------------------------------------------------------------------------
#
# `sci zot export` opens zotero.sqlite with immutable=1, which makes SQLite
# skip WAL processing entirely: every change Zotero desktop has COMMITTED
# but not yet checkpointed is invisible to the dump. sci measures the gap
# rather than closing it — internal/zot/local/db.go's DB.PendingWAL() sizes
# the sibling -wal file and internal/zot/cli/export.go stamps the result
# into the sidecar as `pending_wal_bytes`.
#
# The field is `omitempty`, so its ABSENCE means PendingWAL returned
# ok=false: no -wal file, or a fully checkpointed one. That is the clean
# case, and it is why this gate treats absent and 0 identically. A sidecar
# that is missing entirely is a different thing — the sidecar is written
# LAST, so no sidecar means the export died mid-write.
#
# Verdicts (echoed as "<verdict> <bytes>"):
#   clean     — the dump saw everything Zotero had committed
#   dirty     — N bytes of committed change the dump could not see
#   missing   — no sidecar: the export did not finish
#   malformed — a sidecar that is not JSON, or that describes an empty dump
#
# Exit codes mirror the verdicts (0/1/2/3) so a caller can branch on `$?`.
pl_wal_verdict() {
    local meta=$1
    local tolerance=${2:-0}

    if [[ ! -r $meta ]]; then
        printf 'missing 0\n'
        return 2
    fi
    if ! jq -e . <"$meta" >/dev/null 2>&1; then
        printf 'malformed 0\n'
        return 3
    fi

    # A dump with no items is not a dump. This catches the case where the
    # export produced a syntactically valid but empty mirror — the sidecar
    # parses, the gate would say "clean", and the build would wipe the
    # item plane.
    local items
    items=$(pl_json "$meta" '.stats.items' 0)
    if [[ ! $items =~ ^[0-9]+$ ]] || ((items == 0)); then
        printf 'malformed 0\n'
        return 3
    fi

    local bytes
    bytes=$(pl_json "$meta" '.pending_wal_bytes' 0)
    [[ $bytes =~ ^[0-9]+$ ]] || bytes=0

    if ((bytes > tolerance)); then
        printf 'dirty %s\n' "$bytes"
        return 1
    fi
    printf 'clean %s\n' "$bytes"
    return 0
}

# ---------------------------------------------------------------------------
# The OpenAlex leash
# ---------------------------------------------------------------------------
#
# `sci zot openalex sync` is the one stage that costs money, and it has no
# --limit: it re-fetches the whole library every run, then expands every
# work's referenced_works into the reference title pool. So the question is
# never "how much of it should we run" — it is "may we run it at all".
#
# The cost is a MEASUREMENT, not a guess. sci writes the request count it
# actually made into openalex-works.ndjson.meta.json as `stats.requests`,
# and the cited-works arm's size into openalex-titles.ndjson.meta.json as
# `records_total`. When `stats.requests` is absent (an older sidecar) the
# arm is reconstructed from the counters that are:
#
#   ceil(dois_requested / 50)          DOI lookups, batched 50 per request
#   + dois_unbatchable                 DOIs that had to go one at a time
#   + titles_queried                   one request per DOI-less item
#   + fallback_titles_queried          one per DOI that resolved to nothing
#
# and the cited arm is always ceil(records_total / 50), batched the same 50.
#
# On this corpus that totals ~4,200 requests. A cap of 50 therefore defers,
# and deferring is the correct answer: an automation that quietly bills is
# worse than one that quietly fails. Raise the cap deliberately, or run the
# stage by hand.
#
# Prints an integer, or "unknown" when the sidecars cannot answer. Unknown
# fails CLOSED (see pl_oa_decision) — a cost nobody has measured is not a
# cost anybody chose.
pl_oa_batch=50

pl_oa_estimate() {
    local works_meta=$1 titles_meta=$2
    local works cited

    works=$(pl_json "$works_meta" '.stats.requests' '')
    if [[ ! $works =~ ^[1-9][0-9]*$ ]]; then
        local dois unbatchable titles fallback
        dois=$(pl_json "$works_meta" '.stats.dois_requested' '')
        titles=$(pl_json "$works_meta" '.stats.titles_queried' '')
        unbatchable=$(pl_json "$works_meta" '.stats.dois_unbatchable' 0)
        fallback=$(pl_json "$works_meta" '.stats.fallback_titles_queried' 0)
        if [[ ! $dois =~ ^[0-9]+$ || ! $titles =~ ^[0-9]+$ ]]; then
            printf 'unknown\n'
            return 1
        fi
        [[ $unbatchable =~ ^[0-9]+$ ]] || unbatchable=0
        [[ $fallback =~ ^[0-9]+$ ]] || fallback=0
        works=$(((dois + pl_oa_batch - 1) / pl_oa_batch + unbatchable + titles + fallback))
    fi

    cited=$(pl_json "$titles_meta" '.records_total' '')
    if [[ ! $cited =~ ^[0-9]+$ ]]; then
        printf 'unknown\n'
        return 1
    fi

    printf '%s\n' "$((works + (cited + pl_oa_batch - 1) / pl_oa_batch))"
}

# pl_oa_decision <new_identifiers> <estimate> <cap>
#
# Prints one of:
#   run                    — the delta introduced unresolved identifiers and
#                            the measured cost fits inside the chosen cap
#   skip:no_new_identifiers
#   defer:over_cap
#   defer:unknown_cost
#
# The two conditions are independent on purpose. "Did anything new appear"
# decides whether the stage is WORTH running; "what would it cost" decides
# whether it is ALLOWED to. Collapsing them would let a one-DOI delta
# authorize a four-thousand-request run.
pl_oa_decision() {
    local new=$1 estimate=$2 cap=$3

    [[ $new =~ ^[0-9]+$ ]] || new=0
    if ((new == 0)); then
        printf 'skip:no_new_identifiers\n'
        return 0
    fi
    if [[ ! $estimate =~ ^[0-9]+$ ]]; then
        printf 'defer:unknown_cost\n'
        return 0
    fi
    if ((estimate > cap)); then
        printf 'defer:over_cap\n'
        return 0
    fi
    printf 'run\n'
}

# ---------------------------------------------------------------------------
# Debounce
# ---------------------------------------------------------------------------
#
# A reading-list import moves the Zotero version counter twenty times in a
# minute. The runner fires once for the batch: it records WHEN the counter
# last moved and waits for the library to go quiet.
#
# pl_debounce_ready <last_change_epoch> <now_epoch> <quiet_seconds>
# Exit 0 = fire, 1 = still settling.
pl_debounce_ready() {
    local last=$1 now=$2 quiet=$3
    [[ $last =~ ^[0-9]+$ ]] || return 0 # no recorded change: nothing to wait for
    ((now - last >= quiet))
}
