package bib

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
)

// ErrNotFound is what a [Lookup] returns when the upstream index answered
// definitively that it holds no such work — as opposed to any other error,
// which means the question went unanswered. Verify treats the two very
// differently: not-found is evidence about the citation, a transport failure
// is evidence about the network.
var ErrNotFound = errors.New("bib: reference not found upstream")

// VerifyStatus is the verdict on one unresolved reference.
type VerifyStatus string

// The verdicts Verify assigns. Each maps to a distinct action.
const (
	// StatusAmbiguous — the library holds more than one matching item. The
	// answer is already local; disambiguate by key.
	StatusAmbiguous VerifyStatus = "ambiguous"
	// StatusExternal — a real work, confirmed upstream, that the library
	// doesn't have yet. Add it.
	StatusExternal VerifyStatus = "external"
	// StatusNotFound — every consulted index AND the DOI registry disclaim
	// the identifier. This is the fabricated-citation signal. Its strength
	// depends entirely on the lookup chain being authoritative: a citation
	// index alone has coverage gaps (monographs, unindexed preprints) and
	// would report real works as invented, which is why the CLI pairs one
	// with a registry — see [ChainLookup].
	StatusNotFound VerifyStatus = "not-found"
	// StatusUnchecked — the reference carries no identifier an upstream index
	// can resolve (a cite-key, a wikilink, a plain URL). Absence of a library
	// match says nothing about whether the work is real; a human decides.
	StatusUnchecked VerifyStatus = "unchecked"
	// StatusError — the lookup itself failed. The reference's standing is
	// unknown; re-run rather than acting on it.
	StatusError VerifyStatus = "error"
)

// Match is the upstream record behind a [StatusExternal] verdict — just
// enough to recognize the work and to add it, not the raw OpenAlex firehose.
type Match struct {
	OpenAlexID string `json:"openalex_id,omitempty"`
	DOI        string `json:"doi,omitempty"`
	Title      string `json:"title,omitempty"`
	Year       int    `json:"year,omitempty"`
	Venue      string `json:"venue,omitempty"`
	// Retracted flags a work the upstream index marks as retracted — a
	// citation worth catching before it reaches a manuscript.
	Retracted bool `json:"retracted,omitempty"`
}

// Verified is one unresolved reference plus its verdict.
type Verified struct {
	Unresolved
	Status VerifyStatus `json:"status"`
	// Match is populated only for [StatusExternal].
	Match *Match `json:"match,omitempty"`
	// Fix is a complete, resubmittable command, or empty when no single
	// command resolves the situation.
	Fix string `json:"fix,omitempty"`
	// Error carries the lookup failure for [StatusError].
	Error string `json:"error,omitempty"`
}

// Lookup resolves a reference against an upstream bibliographic index.
// Implementations return [ErrNotFound] for a definitive miss and any other
// error for a failure to ask.
type Lookup interface {
	ResolveRef(ctx context.Context, ref Ref) (*Match, error)
}

// ChainLookup consults several indexes in order and returns the first hit.
//
// This exists because a single index is the wrong authority for "does this
// work exist?". A citation index (OpenAlex) has rich metadata but real
// coverage gaps — monographs, unindexed preprints — while a DOI registry
// answers existence definitively but says little else. Asking the rich index
// first and the authoritative one second gets both: good metadata when it's
// available, and a not-found verdict only when every index agrees.
//
// [ErrNotFound] from one link falls through to the next. Any other error is
// remembered and returned if no later link succeeds — an unanswered question
// must never collapse into "this citation is fabricated".
type ChainLookup []Lookup

// ResolveRef implements [Lookup].
func (c ChainLookup) ResolveRef(ctx context.Context, ref Ref) (*Match, error) {
	var firstErr error
	for _, lk := range c {
		match, err := lk.ResolveRef(ctx, ref)
		switch {
		case err == nil && match != nil:
			return match, nil
		case err != nil && !errors.Is(err, ErrNotFound) && firstErr == nil:
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, ErrNotFound
}

// checkable reports whether a reference kind carries an identifier an
// upstream index can resolve deterministically. Cite-keys and wikilinks are
// local naming conventions and plain URLs aren't bibliographic identifiers —
// looking any of them up would invite a wrong match, so they stay unchecked.
func checkable(k RefKind) bool { return k == KindDOI || k == KindArxiv }

// Verify classifies each unresolved reference, consulting lk only for the
// kinds that carry a resolvable identifier. Input order is preserved.
//
// Lookups run serially: a document's unresolved set is small (single digits
// in practice) and OpenAlex rate-limits aggressively, so the concurrency
// isn't worth the 429s. A cancelled ctx surfaces as [StatusError] per
// reference rather than a partial result.
func Verify(ctx context.Context, unresolved []Unresolved, lk Lookup) []Verified {
	return lo.Map(unresolved, func(u Unresolved, _ int) Verified {
		switch {
		case len(u.Candidates) > 1:
			return Verified{Unresolved: u, Status: StatusAmbiguous}
		case !checkable(u.Kind):
			return Verified{Unresolved: u, Status: StatusUnchecked}
		}
		match, err := lk.ResolveRef(ctx, u.Ref)
		switch {
		case errors.Is(err, ErrNotFound):
			return Verified{Unresolved: u, Status: StatusNotFound}
		case err != nil:
			return Verified{Unresolved: u, Status: StatusError, Error: err.Error()}
		case match == nil:
			// A nil match with a nil error is a Lookup bug; treat it as
			// unanswered rather than as evidence against the citation.
			return Verified{Unresolved: u, Status: StatusError, Error: "lookup returned no match and no error"}
		}
		return Verified{Unresolved: u, Status: StatusExternal, Match: match}
	})
}

// FixCommand returns the complete command that resolves v, or "" when no
// single command does. Per the output contract a Fix must be runnable
// verbatim — so not-found and unchecked, which need human judgement, get
// nothing rather than a command that only looks helpful.
func FixCommand(v Verified, scope string) string {
	switch v.Status {
	case StatusExternal:
		if v.Match == nil {
			return ""
		}
		// item add --openalex takes a DOI, a W-id, an arXiv id, or a PMID.
		id := v.Match.DOI
		if id == "" {
			id = v.Match.OpenAlexID
		}
		if id == "" {
			return ""
		}
		return fmt.Sprintf("sci zot --library %s item add --openalex %s", scope, id)
	case StatusAmbiguous:
		if len(v.Candidates) < 2 {
			return ""
		}
		// `item read` takes one key, so an OR-group search is the only shape
		// that puts every candidate on screen in a single runnable command.
		clauses := lo.Map(v.Candidates, func(k string, _ int) string { return "@key:" + k })
		return fmt.Sprintf("sci zot --library %s search %q", scope, strings.Join(clauses, " | "))
	}
	return ""
}
