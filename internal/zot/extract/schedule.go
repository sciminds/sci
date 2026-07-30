package extract

import (
	"cmp"
	"slices"

	"github.com/samber/lo"
)

// isolateChunkPages is the estimated page count at or above which a
// document is scheduled alone in its own docling invocation, so a
// 300-page scanned monograph can never sit at the head of a chunk of
// short papers. Measured on a live run: 47–332-page OCR scans took
// 20–55 minutes each while papers took 30s–6min; 45 sits under the
// smallest observed slow document (47pp) and above every paper, and a
// false isolation costs only one extra ~30s model load.
const isolateChunkPages = 45

// chunkTargetDocs is how many normal-sized documents share one docling
// invocation. Small enough that a mis-estimated document blocks at most
// a few peers; large enough to amortise the ~20-40s model load.
const chunkTargetDocs = 5

// planChunks orders extraction work longest-processing-time-first (LPT)
// and splits it into chunks: every document whose cost is ≥ isolateAt
// gets a chunk of its own (in descending cost order, so the biggest
// work starts first), and the rest are chunked targetDocs at a time,
// still descending. Chunks are NOT re-sorted by total cost: the input
// order is descending per document, so every isolated document already
// precedes every pooled chunk, which is what LPT wants — re-sorting by
// sum would demote a 332-page singleton below a full pooled chunk.
//
// The returned chunks partition 0..len(costs)-1 exactly — every slot
// appears in exactly one chunk. Workers rely on that to write disjoint
// outcome indices without a lock.
func planChunks(costs []int, isolateAt, targetDocs int) [][]int {
	if len(costs) == 0 {
		return nil
	}
	// lo.Chunk panics on size <= 0; maxDoclingBatch stays the hard cap
	// on how many inputs one docling invocation may scan.
	targetDocs = min(max(targetDocs, 1), maxDoclingBatch)

	order := lo.Range(len(costs))
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(costs[b], costs[a]) })

	big, small := lo.FilterReject(order, func(slot int, _ int) bool { return costs[slot] >= isolateAt })
	chunks := lo.Map(big, func(slot int, _ int) []int { return []int{slot} })
	return append(chunks, lo.Chunk(small, targetDocs)...)
}

// effectiveJobs bounds the worker count by the amount of work
// available: spawning more workers than chunks would just start
// goroutines that exit immediately.
func effectiveJobs(jobs, nChunks int) int {
	return min(max(jobs, 1), nChunks)
}
