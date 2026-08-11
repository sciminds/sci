#!/usr/bin/env bash
# test-pipeline.sh — the gates, against real sidecars.
#
# Every fixture in testdata/ is derived from a sidecar this pipeline
# actually produced, not from a guess about the shape:
#
#   clean.meta.json           verbatim `sci zot export --format ndjson`
#                             output. `pending_wal_bytes` is ABSENT, which
#                             is the omitempty encoding of PendingWAL()
#                             returning ok=false — no -wal, or a fully
#                             checkpointed one.
#   clean-zero.meta.json      the same sidecar with the field present at 0.
#   dirty.meta.json           the same sidecar carrying 4152 — the size of a
#                             real uncheckpointed WAL measured by committing
#                             8 rows to a WAL-mode SQLite with
#                             wal_autocheckpoint=0, the same condition
#                             sci's own internal/zot/local/wal_test.go
#                             builds and the same scale as the 8 invisible
#                             deletions `zot guide` reports.
#   empty-dump.meta.json      a sidecar whose dump holds zero items.
#   malformed.meta.json       the first 120 bytes of a real sidecar — what a
#                             producer killed mid-write leaves behind.
#   oa-works.meta.json        verbatim `sci zot openalex sync` stats,
#                             including the 857 requests it actually made.
#   oa-titles.meta.json       verbatim cited-works sidecar (167,665 records).
#
# Run: ./test-pipeline.sh   (or `just test-pipeline` from the repo root)

set -uo pipefail

SELF_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TD="$SELF_DIR/testdata"
# shellcheck source=./pipeline-lib.sh
source "$SELF_DIR/pipeline-lib.sh"

pass=0 fail=0

ok() {
    local name=$1 got=$2 want=$3
    if [[ $got == "$want" ]]; then
        printf '  ok    %s\n' "$name"
        pass=$((pass + 1))
    else
        printf '  FAIL  %s\n         got  %q\n         want %q\n' "$name" "$got" "$want"
        fail=$((fail + 1))
    fi
}

# verdict <fixture> → "<verdict> <bytes> rc=<code>"
verdict() {
    local out rc=0
    out=$(pl_wal_verdict "$1" "${2:-0}") || rc=$?
    printf '%s rc=%s\n' "$out" "$rc"
}

printf 'WAL gate\n'
ok 'a real export sidecar with no pending_wal_bytes is clean' \
    "$(verdict "$TD/clean.meta.json")" 'clean 0 rc=0'
ok 'an explicit zero is clean' \
    "$(verdict "$TD/clean-zero.meta.json")" 'clean 0 rc=0'
ok 'a measured 4152-byte WAL is dirty and REFUSED' \
    "$(verdict "$TD/dirty.meta.json")" 'dirty 4152 rc=1'
onebyte=$(mktemp -t zotpipe) && trap 'rm -f "$onebyte"' EXIT
jq '.pending_wal_bytes = 1' <"$TD/clean.meta.json" >"$onebyte"
ok 'one byte of uncheckpointed WAL is already dirty' \
    "$(verdict "$onebyte")" 'dirty 1 rc=1'
ok 'a sidecar that does not exist is missing, not clean' \
    "$(verdict "$TD/does-not-exist.meta.json")" 'missing 0 rc=2'
ok 'a truncated sidecar is malformed, not clean' \
    "$(verdict "$TD/malformed.meta.json")" 'malformed 0 rc=3'
ok 'a dump with zero items is malformed, not clean' \
    "$(verdict "$TD/empty-dump.meta.json")" 'malformed 0 rc=3'
ok 'an operator tolerance lets a known-small WAL through' \
    "$(verdict "$TD/dirty.meta.json" 8192)" 'clean 4152 rc=0'
ok 'and a tolerance below the measurement still refuses' \
    "$(verdict "$TD/dirty.meta.json" 4151)" 'dirty 4152 rc=1'

printf '\nRollback-journal gate\n'
# journal-hot.bin and journal-persist-cold.bin are the SAME SQLite rollback
# journal, copied mid-transaction and again after the commit, from a
# database running journal_mode=PERSIST. Both are 8720 bytes: the file
# survives the commit and is invalidated by zeroing its header, which is
# why the size test this replaced held mbp's pipeline every single run.
# journal-mbp-live.bin is the first 4 KB of mbp's actual
# ~/Zotero/zotero.sqlite-journal, taken with Zotero open.
jverdict() {
    local out rc=0
    out=$(pl_journal_verdict "$1") || rc=$?
    printf '%s rc=%s\n' "$out" "$rc"
}

ok 'a journal carrying the SQLite magic is hot and HOLDS the run' \
    "$(jverdict "$TD/journal-hot.bin")" 'hot 8720 rc=1'
ok 'the same journal after commit under PERSIST is cold' \
    "$(jverdict "$TD/journal-persist-cold.bin")" 'cold 8720 rc=0'
ok 'hot and cold are the same size — size cannot be the test' \
    "$(wc -c <"$TD/journal-hot.bin" | tr -d ' ') $(wc -c <"$TD/journal-persist-cold.bin" | tr -d ' ')" \
    '8720 8720'
ok "mbp's live journal, with Zotero open, is cold" \
    "$(jverdict "$TD/journal-mbp-live.bin")" 'cold 4096 rc=0'
ok 'no journal file at all is absent, not hot' \
    "$(jverdict "$TD/journal-does-not-exist.bin")" 'absent 0 rc=0'
ok 'an empty journal is cold' \
    "$(jverdict "$TD/journal-empty.bin")" 'cold 0 rc=0'
ok 'a journal too short to hold the magic is cold, not hot' \
    "$(jverdict "$TD/journal-truncated.bin")" 'cold 4 rc=0'

printf '\nOpenAlex cost estimate\n'
# 857 (measured works arm) + ceil(167665/50)=3354 = 4211
ok 'the measured request count plus the cited arm' \
    "$(pl_oa_estimate "$TD/oa-works.meta.json" "$TD/oa-titles.meta.json")" '4211'
# A sidecar predating stats.requests reconstructs the arm:
# ceil(5158/50)=104 + 29 unbatchable + 631 titles + 94 fallback = 858,
# within one request of the 857 sci recorded — the formula is checked
# against sci's own accounting, not against itself.
ok 'a legacy sidecar reconstructs the arm from its counters' \
    "$(pl_oa_estimate "$TD/oa-works-legacy.meta.json" "$TD/oa-titles.meta.json")" '4212'
ok 'a sidecar with no stats at all is unknown, never zero' \
    "$(pl_oa_estimate "$TD/oa-works-unmeasured.meta.json" "$TD/oa-titles.meta.json")" 'unknown'
ok 'a missing cited sidecar is unknown, never zero' \
    "$(pl_oa_estimate "$TD/oa-works.meta.json" "$TD/nope.meta.json")" 'unknown'
# A targeted sync writes its OWN spend into stats — one request, honestly.
# Pricing a full re-sync from that number is how a two-request run comes to
# authorise a four-thousand-request one, so the carried measurement wins.
ok 'a delta sidecar prices the full sync from the measurement it carried' \
    "$(pl_oa_estimate "$TD/oa-works-delta.meta.json" "$TD/oa-titles.meta.json")" '4211'
ok 'a delta that carried no full-sync measurement is unknown, not cheap' \
    "$(pl_oa_estimate "$TD/oa-works-delta-uncarried.meta.json" "$TD/oa-titles.meta.json")" 'unknown'

printf '\nOpenAlex leash\n'
ok 'no new DOIs: the metered stage does not run' \
    "$(pl_oa_decision 0 4211 50)" 'skip:no_new_identifiers'
ok 'new DOIs but the measured cost exceeds the chosen cap: defer' \
    "$(pl_oa_decision 3 4211 50)" 'defer:over_cap'
ok 'an unmeasurable cost fails CLOSED' \
    "$(pl_oa_decision 3 unknown 50)" 'defer:unknown_cost'
ok 'new DOIs and a cost inside the cap: run' \
    "$(pl_oa_decision 3 44 50)" 'run'
ok 'exactly at the cap is allowed' \
    "$(pl_oa_decision 1 50 50)" 'run'
ok 'one over the cap is not' \
    "$(pl_oa_decision 1 51 50)" 'defer:over_cap'
ok 'a cap raised past the real cost authorises the real run' \
    "$(pl_oa_decision 3 4211 5000)" 'run'

printf '\nDebounce\n'
now=1000000
ok 'a burst still moving does not fire' \
    "$(pl_debounce_ready $((now - 60)) "$now" 300 && echo fire || echo wait)" 'wait'
ok 'a library gone quiet fires' \
    "$(pl_debounce_ready $((now - 301)) "$now" 300 && echo fire || echo wait)" 'fire'
ok 'exactly the quiet period fires' \
    "$(pl_debounce_ready $((now - 300)) "$now" 300 && echo fire || echo wait)" 'fire'

printf '\n%d passed, %d failed\n' "$pass" "$fail"
((fail == 0))
