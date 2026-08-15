package cmdutil

// suggest.go — "did you mean …?" scoring for mistyped command and flag names.
//
// urfave/cli ships its own suggester (cli.SuggestCommand), and sci used it
// until it started giving confidently wrong answers on the commands people
// mistype most: `zto` suggested `tools` rather than `zot`, `serach` suggested
// `learn` rather than `search`, and anything unrecognizable suggested `h` —
// the one-letter alias of the help command, which is never a useful thing to
// type. Two independent flaws produce that:
//
//  1. Jaro-Winkler (urfave's metric) counts a *swapped adjacent pair* as two
//     independent misses. Transposition is the single most common typing
//     error, so the metric is weakest exactly where a suggester earns its
//     keep. Damerau-Levenshtein counts a swap as one edit, which is what a
//     reader means by "close".
//  2. urfave always returns the nearest candidate, however far away it is.
//     There is no such thing as "no idea" — so a token that resembles nothing
//     still gets an answer, and a wrong answer costs the reader more than
//     silence does.
//
// So: our own metric, and a similarity floor below which we say nothing.

import (
	"maps"
	"slices"

	"github.com/samber/lo"
	"github.com/urfave/cli/v3"
)

// minSuggestionSimilarity is the floor a candidate must clear before we
// volunteer it. Similarity is 1 - editDistance/longestLength, so 0.6 means
// "at most 40% of the longer name had to change". Calibrated against the
// real typos: one swapped pair in a 3-letter name (`zto` → `zot`) scores
// 0.67 and survives, while `nosuchcommand` matches nothing in the tree and
// is correctly left without a suggestion.
const minSuggestionSimilarity = 0.6

// bestMatch returns the candidate closest to typed, or "" when nothing is
// close enough to be worth saying. Single-character candidates are never
// suggested: they are aliases (`h`, `v`), and pointing a confused reader at
// one letter helps nobody.
func bestMatch(candidates []string, typed string) string {
	best, bestScore := "", minSuggestionSimilarity
	for _, c := range candidates {
		if len(c) < 2 {
			continue
		}
		if score := similarity(c, typed); score > bestScore {
			best, bestScore = c, score
		}
	}
	return best
}

// similarity scores two names from 0 (nothing alike) to 1 (identical) as
// 1 - editDistance/longestLength.
func similarity(a, b string) float64 {
	if a == b {
		return 1
	}
	longest := max(len([]rune(a)), len([]rune(b)))
	if longest == 0 {
		return 1
	}
	return 1 - float64(editDistance(a, b))/float64(longest)
}

// editDistance is the Damerau-Levenshtein distance in its optimal
// string-alignment form: the number of insertions, deletions, substitutions
// and adjacent transpositions needed to turn a into b. The transposition case
// is the whole reason we don't use urfave's metric — it is what makes `zto`
// one edit from `zot` instead of two.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	// d[i][j] = distance between the first i runes of a and the first j of b.
	d := make([][]int, len(ar)+1)
	for i := range d {
		d[i] = make([]int, len(br)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ar); i++ {
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			d[i][j] = min(
				d[i-1][j]+1,      // deletion
				d[i][j-1]+1,      // insertion
				d[i-1][j-1]+cost, // substitution
			)
			if i > 1 && j > 1 && ar[i-1] == br[j-2] && ar[i-2] == br[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1) // transposition
			}
		}
	}
	return d[len(ar)][len(br)]
}

// suggestSubcommand returns the visible child of cmd closest to typed, or ""
// when none is close enough. Aliases count as candidates; hidden commands
// don't (suggesting something the help page never lists is a dead end).
func suggestSubcommand(cmd *cli.Command, typed string) string {
	visible := lo.Filter(cmd.Commands, func(sub *cli.Command, _ int) bool { return !sub.Hidden })
	names := lo.FlatMap(visible, func(sub *cli.Command, _ int) []string { return sub.Names() })
	return bestMatch(names, typed)
}

// suggestFlagName returns the flag of cmd closest to typed (without leading
// dashes), or "" when none is close enough.
func suggestFlagName(cmd *cli.Command, typed string) string {
	names := lo.FlatMap(cmd.Flags, func(f cli.Flag, _ int) []string { return f.Names() })
	return bestMatch(names, typed)
}

// descendantPaths returns the full command paths of every grandchild of cmd
// named like typed — exactly if any match exactly, otherwise the closest
// spelling that clears the suggestion floor. Empty when nothing is close.
//
// This is the second look, taken only after no direct child matched. Two real
// cases motivate it, and neither is answerable from the sibling list alone:
//
//   - `sci convert` — nothing at the top level is spelled anything like it,
//     but `sci db convert`, `sci py convert` and `sci vid convert` all exist.
//     Listing all three is the honest answer.
//   - `sci serach` — the flagship command is `sci zot search`, one level down,
//     so a top-level suggester can only ever pick something wrong.
//
// One level, not the whole tree: it keeps the search cheap and the answer
// short, and sci's genuinely-mistyped commands live at depth 1 or 2.
func descendantPaths(cmd *cli.Command, typed string) []string {
	grandchildren := map[string][]string{}
	for _, child := range cmd.Commands {
		if child.Hidden {
			continue
		}
		for _, gc := range child.Commands {
			if gc.Hidden || gc.Name == "help" {
				continue
			}
			grandchildren[gc.Name] = append(grandchildren[gc.Name], gc.FullName())
		}
	}
	if paths, ok := grandchildren[typed]; ok {
		return paths
	}
	if near := bestMatch(slices.Sorted(maps.Keys(grandchildren)), typed); near != "" {
		return grandchildren[near]
	}
	return nil
}
