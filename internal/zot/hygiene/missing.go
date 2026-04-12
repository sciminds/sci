package hygiene

import (
	"fmt"
	"sort"

	"github.com/sciminds/cli/internal/zot/local"
)

// MissingField names a field the Missing check knows how to inspect. The
// values double as the stable Kind emitted in Findings and on the command
// line (`zot doctor missing --field doi,abstract`).
type MissingField string

const (
	FieldTitle    MissingField = "title"
	FieldCreators MissingField = "creators"
	FieldDOI      MissingField = "doi"
	FieldAbstract MissingField = "abstract"
	FieldDate     MissingField = "date"
	FieldURL      MissingField = "url"
	FieldPDF      MissingField = "pdf"
	FieldTags     MissingField = "tags"
)

// AllMissingFields is the default field set when the caller passes none.
// Order is intentional: structural fields (title, creators) first so they
// lead human output, then citation-affecting (date), then coverage fields.
var AllMissingFields = []MissingField{
	FieldTitle, FieldCreators, FieldDate,
	FieldDOI, FieldAbstract, FieldURL, FieldPDF, FieldTags,
}

// ParseMissingField maps a user-facing string to a MissingField, returning
// an error on unknown input so CLI callers can surface a clean usage error.
func ParseMissingField(s string) (MissingField, error) {
	for _, f := range AllMissingFields {
		if string(f) == s {
			return f, nil
		}
	}
	return "", fmt.Errorf("unknown field %q (want one of: title, creators, date, doi, abstract, url, pdf, tags)", s)
}

// severityFor grades a missing-field finding. Structural fields that make
// a record unusable are errors; fields that break citations or sorting
// are warnings; the rest are info-level coverage gaps.
func severityFor(f MissingField) Severity {
	switch f {
	case FieldTitle:
		return SevError
	case FieldCreators, FieldDate:
		return SevWarn
	default:
		return SevInfo
	}
}

// MissingStats aggregates per-field coverage across the scanned population.
// Attached to Report.Stats so the result renderer can print a coverage table
// without re-walking the findings slice.
type MissingStats struct {
	Scanned  int             `json:"scanned"`
	Coverage []FieldCoverage `json:"coverage"`
}

// FieldCoverage is the presence count for a single field. PercentPresent is
// precomputed so JSON consumers and the human renderer stay in sync.
type FieldCoverage struct {
	Field          string  `json:"field"`
	Present        int     `json:"present"`
	Missing        int     `json:"missing"`
	PercentPresent float64 `json:"percent_present"`
}

// Missing scans the library and emits one Finding per (item, missing field)
// pair. Pass nil or empty fields to check all known fields.
//
// Findings are sorted by item key for stable output. The caller decides
// whether to render them all, truncate, or JSON-dump.
func Missing(db local.Reader, fields []MissingField) (*Report, error) {
	if len(fields) == 0 {
		fields = AllMissingFields
	}

	rows, err := db.ScanFieldPresence()
	if err != nil {
		return nil, err
	}

	present := map[MissingField]int{}
	var findings []Finding

	for _, r := range rows {
		for _, f := range fields {
			ok := fieldPresent(r, f)
			if ok {
				present[f]++
				continue
			}
			findings = append(findings, Finding{
				Check:    "missing",
				Kind:     string(f),
				ItemKey:  r.Key,
				Title:    r.Title,
				Severity: severityFor(f),
				Message:  "missing " + string(f),
				Fixable:  false,
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].ItemKey != findings[j].ItemKey {
			return findings[i].ItemKey < findings[j].ItemKey
		}
		return findings[i].Kind < findings[j].Kind
	})

	cov := make([]FieldCoverage, 0, len(fields))
	for _, f := range fields {
		p := present[f]
		m := len(rows) - p
		var pct float64
		if len(rows) > 0 {
			pct = 100 * float64(p) / float64(len(rows))
		}
		cov = append(cov, FieldCoverage{
			Field:          string(f),
			Present:        p,
			Missing:        m,
			PercentPresent: pct,
		})
	}

	return &Report{
		Check:    "missing",
		Scanned:  len(rows),
		Findings: findings,
		Stats: MissingStats{
			Scanned:  len(rows),
			Coverage: cov,
		},
	}, nil
}

func fieldPresent(r local.ItemFieldPresence, f MissingField) bool {
	switch f {
	case FieldTitle:
		return r.HasTitle
	case FieldCreators:
		return r.CreatorCount > 0
	case FieldDOI:
		return r.HasDOI
	case FieldAbstract:
		return r.HasAbstract
	case FieldDate:
		return r.HasDate
	case FieldURL:
		return r.HasURL
	case FieldPDF:
		return r.PDFCount > 0
	case FieldTags:
		return r.TagCount > 0
	}
	return false
}
