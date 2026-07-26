package cli

// warnings.go holds the success-path warning rules for zot: data-quality and
// freshness caveats volunteered on results agents already asked for. The
// dabble/gundam evidence is blunt — models never run optional diagnostic
// flags, so anything the tool won't say inline effectively doesn't exist.
// Rules fail open: a warning must never fail (or slow) a run, and a missing
// signal (no lastsync row) means no claim, not a warning.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/sciminds/cli/internal/cmdutil"
	"github.com/sciminds/cli/internal/zot/local"
)

// staleLocalDaysDefault is how far behind the local zotero.sqlite may lag
// before local reads carry a stale-local warning. Override with the
// SCI_ZOT_STALE_DAYS env var (0 disables the rule entirely).
const staleLocalDaysDefault = 14

// staleThreshold resolves the active staleness window. Returns ok=false when
// the rule is disabled.
func staleThreshold() (time.Duration, bool) {
	days := staleLocalDaysDefault
	if raw := os.Getenv("SCI_ZOT_STALE_DAYS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			days = n
		}
	}
	if days <= 0 {
		return 0, false
	}
	return time.Duration(days) * 24 * time.Hour, true
}

// staleLocalWarning checks the local DB's lastsync age and returns a
// stale-local warning when it exceeds the threshold. fix, when non-empty, is
// the complete command to resubmit for API ground truth (callers with a
// --remote twin pass it; bib/export have none and pass "").
func staleLocalWarning(db local.Reader, fix string) []cmdutil.Warning {
	threshold, enabled := staleThreshold()
	if !enabled {
		return nil
	}
	last, ok := db.LastSync()
	if !ok {
		return nil
	}
	age := time.Since(last)
	if age < threshold {
		return nil
	}
	days := int(age.Hours() / 24)
	return []cmdutil.Warning{{
		Code: cmdutil.CodeStaleLocal,
		Message: fmt.Sprintf(
			"local Zotero DB last synced %d days ago — results may be missing recent items (open Zotero desktop to sync)", days),
		Fix: fix,
	}}
}

// remoteRerunFix rebuilds the current command line with --remote appended —
// the ground-truth resubmit for a stale-local warning. Empty when argv has
// no zot token (test binaries, exotic invocations): no fix beats a wrong fix.
func remoteRerunFix(argv []string) string {
	if len(argv) < 2 || !lo.Contains(argv[1:], "zot") {
		return ""
	}
	parts := lo.Map(argv[1:], func(arg string, _ int) string { return shellQuote(arg) })
	return "sci " + strings.Join(parts, " ") + " --remote"
}

// codeBibQuality labels the bibliography-quality warning. Error codes are a
// closed cmdutil vocabulary agents branch on; warning codes are labels and
// may be domain-local like this one.
const codeBibQuality cmdutil.Code = "bib-quality"

// bibQualityWarning flags resolved bibliography items whose records are
// citation-broken (no date → mangled BibTeX year). Only fields present in
// the list shape are checked — honesty over coverage.
func bibQualityWarning(resolved []local.Item, scope string) []cmdutil.Warning {
	undated := lo.FilterMap(resolved, func(it local.Item, _ int) (string, bool) {
		return it.Key, strings.TrimSpace(it.Date) == ""
	})
	if len(undated) == 0 {
		return nil
	}
	sample := undated
	if len(sample) > 5 {
		sample = sample[:5]
	}
	return []cmdutil.Warning{{
		Code: codeBibQuality,
		Message: fmt.Sprintf(
			"%d of %d resolved items have no date — their bibliography entries will lack a year (keys: %s)",
			len(undated), len(resolved), strings.Join(sample, ", ")),
		Fix: fmt.Sprintf("sci zot --library %s doctor missing", scope),
	}}
}

// scopeFromCtx returns the resolved library scope for building fix commands,
// defaulting to personal when unresolved (tests, bypassed hooks).
func scopeFromCtx(ctx context.Context) string {
	if h := libraryHolderFromCtx(ctx); h != nil && h.Resolved != nil {
		return string(h.Resolved.Scope)
	}
	return "personal"
}
