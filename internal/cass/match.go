package cass

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/ui"
)

// MatchCandidate pairs a Canvas student with a match score.
type MatchCandidate struct {
	Student Student
	Score   int // 0-100, 100 = exact normalized match
}

// MatchPair records a confirmed match between a GitHub name and a Canvas student.
type MatchPair struct {
	GHName  string
	Student Student
}

var (
	nonAlphaRe   = regexp.MustCompile(`[^a-z ]+`)  // strips non-alphabetic chars (keeps spaces)
	multiSpaceRe = regexp.MustCompile(`\s+`)       // collapses whitespace runs
	slugSepRe    = regexp.MustCompile(`[\s_/()]+`) // splits on slug separators
	multiDashRe  = regexp.MustCompile(`-{2,}`)     // collapses consecutive dashes
)

// NormalizeName normalizes a name for matching:
//   - lowercase, strip whitespace
//   - swap "Last, First" → "First Last"
//   - remove non-alphabetic chars (keep spaces)
//   - sort tokens alphabetically
func NormalizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}

	// Handle "Last, First" format.
	if idx := strings.Index(s, ","); idx >= 0 {
		last := strings.TrimSpace(s[:idx])
		first := strings.TrimSpace(s[idx+1:])
		s = first + " " + last
	}

	// Remove non-alpha chars (keep spaces).
	s = nonAlphaRe.ReplaceAllString(s, "")
	s = multiSpaceRe.ReplaceAllString(strings.TrimSpace(s), " ")

	// Sort tokens for order-independent matching.
	tokens := strings.Fields(s)
	slices.Sort(tokens)
	return strings.Join(tokens, " ")
}

// Slugify creates a URL-safe slug from a name.
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugSepRe.ReplaceAllString(s, "-")
	s = multiDashRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	// Remove chars that aren't alphanumeric or dash.
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	return multiDashRe.ReplaceAllString(result, "-")
}

// FindCandidates returns Canvas students ranked by match quality for a GitHub name.
// Only returns candidates with score > 0.
func FindCandidates(ghName string, canvasStudents []Student) []MatchCandidate {
	normGH := NormalizeName(ghName)
	if normGH == "" {
		return nil
	}
	ghTokens := strings.Fields(normGH)

	var candidates []MatchCandidate
	for _, cs := range canvasStudents {
		normCanvas := NormalizeName(cs.Name)
		if normCanvas == "" {
			continue
		}

		score := scoreName(normGH, ghTokens, normCanvas)
		if score > 0 {
			candidates = append(candidates, MatchCandidate{Student: cs, Score: score})
		}
	}

	slices.SortFunc(candidates, func(a, b MatchCandidate) int {
		return cmp.Compare(b.Score, a.Score) // descending
	})
	return candidates
}

// scoreName returns a match score between 0-100.
func scoreName(normGH string, ghTokens []string, normCanvas string) int {
	// Exact match after normalization.
	if normGH == normCanvas {
		return 100
	}

	canvasTokens := strings.Fields(normCanvas)

	// Token subset match: check if GH tokens are a subset of Canvas tokens (or vice versa).
	ghSet := toSet(ghTokens)
	canvasSet := toSet(canvasTokens)

	overlap := setIntersection(ghSet, canvasSet)
	if len(overlap) == 0 {
		return 0
	}

	// Score based on overlap ratio.
	shorter := len(ghSet)
	if len(canvasSet) < shorter {
		shorter = len(canvasSet)
	}

	// Require at least 50% overlap of the shorter set.
	ratio := float64(len(overlap)) / float64(shorter)
	if ratio < 0.5 {
		return 0
	}

	return int(ratio * 90) // max 90 for partial match, 100 reserved for exact
}

func toSet(tokens []string) map[string]bool {
	return lo.SliceToMap(tokens, func(t string) (string, bool) {
		return t, true
	})
}

func setIntersection(a, b map[string]bool) map[string]bool {
	result := make(map[string]bool)
	for k := range a {
		if b[k] {
			result[k] = true
		}
	}
	return result
}

// AutoMatch performs automatic matching of GitHub names to Canvas students.
// Returns confirmed matches (score >= 100) and unmatched GitHub names.
func AutoMatch(ghNames []string, canvasStudents []Student) (matched []MatchPair, unmatched []string) {
	used := make(map[int]bool) // canvas IDs already matched

	for _, ghName := range ghNames {
		candidates := FindCandidates(ghName, canvasStudents)

		var best *MatchCandidate
		for i := range candidates {
			if !used[candidates[i].Student.CanvasID] {
				best = &candidates[i]
				break
			}
		}

		if best != nil && best.Score >= 100 {
			matched = append(matched, MatchPair{GHName: ghName, Student: best.Student})
			used[best.Student.CanvasID] = true
		} else {
			unmatched = append(unmatched, ghName)
		}
	}
	return matched, unmatched
}

// MatchResult is the output of RunMatch.
type MatchResult struct {
	Matched   int      `json:"matched"`
	Unmatched int      `json:"unmatched"`
	Details   []string `json:"details,omitempty"`
}

func (r *MatchResult) JSON() any { return r }
func (r *MatchResult) Human() string {
	var b strings.Builder
	for _, d := range r.Details {
		fmt.Fprintf(&b, "  %s\n", d)
	}
	if r.Matched > 0 {
		fmt.Fprintf(&b, "\n  %s %d student(s) matched\n", ui.SymOK, r.Matched)
	}
	if r.Unmatched > 0 {
		fmt.Fprintf(&b, "  %s %d student(s) still unmatched\n", ui.SymWarn, r.Unmatched)
	}
	if r.Matched == 0 && r.Unmatched == 0 {
		fmt.Fprintf(&b, "  %s All students already matched.\n", ui.SymOK)
	}
	return b.String()
}

// RunMatch matches GitHub usernames to Canvas students.
// With autoOnly=true, only exact matches are applied. Otherwise, interactive prompts
// are shown for ambiguous matches using huh select forms.
func RunMatch(db *DB, autoOnly bool) (*MatchResult, error) {
	students, err := db.AllStudents()
	if err != nil {
		return nil, err
	}

	// Find students that need matching (have no github_username).
	unmatched := lo.Filter(students, func(s Student, _ int) bool {
		return s.GitHubUsername == ""
	})

	if len(unmatched) == 0 {
		if err := db.SetMeta("match_pending", "false"); err != nil {
			return nil, fmt.Errorf("clear match_pending: %w", err)
		}
		return &MatchResult{}, nil
	}

	// Get GitHub names from submissions or gh_slug data.
	// For now, look at submissions with source=github for distinct usernames not yet matched.
	var ghNames []string
	err = db.db.NewQuery(`
		SELECT DISTINCT s.repo_name FROM submissions s
		WHERE s.source = 'github' AND s.repo_name IS NOT NULL AND s.repo_name != ''
	`).Column(&ghNames)
	if err != nil || len(ghNames) == 0 {
		// No GitHub data to match against — just report unmatched Canvas students.
		return &MatchResult{Unmatched: len(unmatched)}, nil
	}

	// Auto-match exact name matches.
	matched, remaining := AutoMatch(ghNames, unmatched)

	result := &MatchResult{}

	// Apply auto-matches.
	for _, m := range matched {
		if err := db.SetStudentGitHubUsername(m.Student.CanvasID, m.GHName); err != nil {
			return nil, fmt.Errorf("save match: %w", err)
		}
		result.Matched++
		result.Details = append(result.Details, fmt.Sprintf("%s → %s (auto)", m.GHName, m.Student.Name))
	}

	if autoOnly {
		result.Unmatched = len(remaining)
		pending := "false"
		if result.Unmatched > 0 {
			pending = "true"
		}
		if err := db.SetMeta("match_pending", pending); err != nil {
			return nil, fmt.Errorf("set match_pending: %w", err)
		}
		return result, nil
	}

	// Interactive matching for remaining.
	// Track matched Canvas student IDs to filter candidates.
	matchedIDs := lo.SliceToMap(matched, func(m MatchPair) (int, bool) {
		return m.Student.CanvasID, true
	})

	// Build the available pool once; use matchedIDs to filter during candidate generation.
	availableCanvas := func() []Student {
		return lo.Reject(unmatched, func(s Student, _ int) bool {
			return matchedIDs[s.CanvasID]
		})
	}

	for _, ghName := range remaining {
		candidates := FindCandidates(ghName, availableCanvas())
		if len(candidates) == 0 {
			result.Unmatched++
			result.Details = append(result.Details, fmt.Sprintf("%s → no candidates found", ghName))
			continue
		}

		// Build options for huh select.
		options := make([]huh.Option[int], 0, len(candidates)+1)
		for _, c := range candidates {
			label := fmt.Sprintf("%s (%d%%)", c.Student.Name, c.Score)
			options = append(options, huh.NewOption(label, c.Student.CanvasID))
		}
		options = append(options, huh.NewOption("Skip", -1))

		var choice int
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[int]().
				Title(fmt.Sprintf("Match GitHub user: %s", ghName)).
				Options(options...).
				Value(&choice),
		)).WithTheme(ui.HuhTheme()).WithKeyMap(ui.HuhKeyMap()).Run(); err != nil {
			return result, err
		}

		if choice == -1 {
			result.Unmatched++
			result.Details = append(result.Details, fmt.Sprintf("%s → skipped", ghName))
			continue
		}

		if err := db.SetStudentGitHubUsername(choice, ghName); err != nil {
			return nil, fmt.Errorf("save match: %w", err)
		}
		result.Matched++
		matchedIDs[choice] = true
		for _, c := range candidates {
			if c.Student.CanvasID == choice {
				result.Details = append(result.Details, fmt.Sprintf("%s → %s (manual)", ghName, c.Student.Name))
				break
			}
		}
	}

	pending := "false"
	if result.Unmatched > 0 {
		pending = "true"
	}
	if err := db.SetMeta("match_pending", pending); err != nil {
		return nil, fmt.Errorf("set match_pending: %w", err)
	}

	return result, nil
}
