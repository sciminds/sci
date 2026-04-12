package hygiene

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sciminds/cli/internal/zot/local"
)

// InvalidField names a field the Invalid check knows how to validate.
// Values are Zotero's internal field names (case-sensitive).
type InvalidField string

const (
	InvalidFieldDOI  InvalidField = "DOI"
	InvalidFieldISBN InvalidField = "ISBN"
	InvalidFieldURL  InvalidField = "url"
	InvalidFieldDate InvalidField = "date"
)

// AllInvalidFields is the default field set when the caller passes none.
var AllInvalidFields = []InvalidField{
	InvalidFieldDOI, InvalidFieldISBN, InvalidFieldURL, InvalidFieldDate,
}

// InvalidFieldsAsStrings returns the string form expected by
// local.ScanFieldValues.
func InvalidFieldsAsStrings(fs []InvalidField) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = string(f)
	}
	return out
}

// ParseInvalidField maps a user-facing string to an InvalidField.
// Case-insensitive match for convenience — Zotero's field names are
// mixed-case but users shouldn't have to remember that.
func ParseInvalidField(s string) (InvalidField, error) {
	ls := strings.ToLower(strings.TrimSpace(s))
	for _, f := range AllInvalidFields {
		if strings.ToLower(string(f)) == ls {
			return f, nil
		}
	}
	return "", &unknownFieldError{what: "field", got: s, want: "doi, isbn, url, date"}
}

type unknownFieldError struct {
	what, got, want string
}

func (e *unknownFieldError) Error() string {
	return "unknown " + e.what + " " + strconv.Quote(e.got) + " (want " + e.want + ")"
}

// InvalidStats is the summary attached to Report.Stats for invalid runs.
// Mirrors MissingStats in spirit — one row per validated field with the
// count of good/bad values and the percentage.
type InvalidStats struct {
	Scanned  int                    `json:"scanned"`
	PerField []InvalidFieldCoverage `json:"per_field"`
}

// InvalidFieldCoverage is how many rows for a given field passed
// validation vs. failed. Scanned reflects rows seen for that field only,
// not the overall population.
type InvalidFieldCoverage struct {
	Field       string  `json:"field"`
	Scanned     int     `json:"scanned"`
	Bad         int     `json:"bad"`
	PercentGood float64 `json:"percent_good"`
}

// validateField dispatches to the per-field validator. Fields outside
// the known set return (true, "") so callers can pass raw scan output
// without pre-filtering.
func validateField(field, value string) (bool, string) {
	switch InvalidField(field) {
	case InvalidFieldDOI:
		return ValidateDOI(value)
	case InvalidFieldISBN:
		return ValidateISBN(value)
	case InvalidFieldURL:
		return ValidateURL(value)
	case InvalidFieldDate:
		return ValidateDate(value)
	}
	return true, ""
}

// InvalidFromFieldValues is the pure, DB-free entry point. Feed it a
// slice of FieldValue rows (from local.ScanFieldValues or hand-built in
// tests) and it returns a Report with one Finding per invalid row.
func InvalidFromFieldValues(rows []local.FieldValue) *Report {
	findings := make([]Finding, 0)
	perField := map[string]*InvalidFieldCoverage{}

	for _, r := range rows {
		// Skip fields we don't validate so unknown columns in a larger
		// scan don't inflate counts.
		if _, known := knownField(r.Field); !known {
			continue
		}
		cov := perField[r.Field]
		if cov == nil {
			cov = &InvalidFieldCoverage{Field: r.Field}
			perField[r.Field] = cov
		}
		cov.Scanned++
		ok, reason := validateField(r.Field, r.Value)
		if ok {
			continue
		}
		cov.Bad++
		findings = append(findings, Finding{
			Check:    "invalid",
			Kind:     r.Field,
			ItemKey:  r.Key,
			Title:    r.Title,
			Severity: SevWarn,
			Message:  r.Field + ": " + reason,
			Fixable:  false,
		})
	}

	// Sort findings for stable output: by item key, then field.
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].ItemKey != findings[j].ItemKey {
			return findings[i].ItemKey < findings[j].ItemKey
		}
		return findings[i].Kind < findings[j].Kind
	})

	// Build per-field coverage in AllInvalidFields order so the renderer
	// shows known fields even when they had zero rows.
	coverage := make([]InvalidFieldCoverage, 0, len(AllInvalidFields))
	for _, f := range AllInvalidFields {
		if cov, ok := perField[string(f)]; ok {
			pct := 0.0
			if cov.Scanned > 0 {
				pct = 100 * float64(cov.Scanned-cov.Bad) / float64(cov.Scanned)
			}
			cov.PercentGood = pct
			coverage = append(coverage, *cov)
		}
	}

	return &Report{
		Check:    "invalid",
		Scanned:  len(rows),
		Findings: findings,
		Stats: InvalidStats{
			Scanned:  len(rows),
			PerField: coverage,
		},
	}
}

// knownField reports whether field is in the validator set and returns
// the typed form.
func knownField(field string) (InvalidField, bool) {
	for _, f := range AllInvalidFields {
		if string(f) == field {
			return f, true
		}
	}
	return "", false
}

// Invalid is the DB-backed orchestrator. It scans for the requested
// fields (or all known fields if nil), validates each value, and returns
// a Report.
func Invalid(db local.Reader, fields []InvalidField) (*Report, error) {
	if len(fields) == 0 {
		fields = AllInvalidFields
	}
	rows, err := db.ScanFieldValues(InvalidFieldsAsStrings(fields))
	if err != nil {
		return nil, err
	}
	return InvalidFromFieldValues(rows), nil
}

// Validators return (ok, reason). When ok is false, reason is a short
// human-readable explanation suitable for inclusion in a Finding message.
// Empty input always fails with reason "empty" — hygiene.Invalid filters
// empties out before calling so this is a defensive fallback.

// doiRegex follows Crossref's recommended pattern: the "10." prefix, a
// 4–9 digit registrant, a slash, then one or more URI-safe chars. Real
// DOIs can contain `< > ; ( )` so we keep the allowed-char set loose.
var doiRegex = regexp.MustCompile(`^10\.\d{4,9}/[-._;()/:A-Za-z0-9<>]+$`)

// ValidateDOI checks that the value looks like a DOI. Accepts raw DOIs
// or DOIs with the common URL prefixes that Zotero and imports use.
func ValidateDOI(raw string) (bool, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false, "empty"
	}
	// Strip common URL prefixes — doi.org / dx.doi.org / "doi:".
	lower := strings.ToLower(s)
	for _, prefix := range []string{
		"https://doi.org/",
		"http://doi.org/",
		"https://dx.doi.org/",
		"http://dx.doi.org/",
		"doi:",
	} {
		if strings.HasPrefix(lower, prefix) {
			s = s[len(prefix):]
			break
		}
	}
	if !doiRegex.MatchString(s) {
		return false, "does not match 10.NNNN/suffix pattern"
	}
	return true, ""
}

// ValidateISBN checks that the value is a valid ISBN-10 or ISBN-13 with
// a correct checksum. Hyphens and whitespace are ignored.
func ValidateISBN(raw string) (bool, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false, "empty"
	}
	// Strip hyphens and whitespace.
	var digits []byte
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits = append(digits, byte(r))
		case r == 'X' || r == 'x':
			digits = append(digits, 'X') // normalize case
		case r == '-' || r == ' ':
			// skip
		default:
			return false, "contains non-ISBN characters"
		}
	}
	switch len(digits) {
	case 10:
		return checkISBN10(digits)
	case 13:
		return checkISBN13(digits)
	default:
		return false, "not 10 or 13 digits"
	}
}

// checkISBN10 verifies an ISBN-10 check digit. The weighted sum of the
// first 9 digits + checksum (where X = 10) must be divisible by 11.
func checkISBN10(d []byte) (bool, string) {
	sum := 0
	for i := 0; i < 9; i++ {
		if d[i] < '0' || d[i] > '9' {
			return false, "non-digit in body"
		}
		sum += int(d[i]-'0') * (10 - i)
	}
	var check int
	if d[9] == 'X' {
		check = 10
	} else if d[9] >= '0' && d[9] <= '9' {
		check = int(d[9] - '0')
	} else {
		return false, "invalid ISBN-10 check digit"
	}
	if (sum+check)%11 != 0 {
		return false, "ISBN-10 checksum mismatch"
	}
	return true, ""
}

// checkISBN13 verifies an ISBN-13 check digit. The weighted sum (alternating
// 1× and 3× for digits 1–12) plus the checksum must be divisible by 10.
func checkISBN13(d []byte) (bool, string) {
	for _, b := range d {
		if b < '0' || b > '9' {
			return false, "ISBN-13 must be digits only"
		}
	}
	sum := 0
	for i := 0; i < 12; i++ {
		n := int(d[i] - '0')
		if i%2 == 0 {
			sum += n
		} else {
			sum += n * 3
		}
	}
	check := (10 - sum%10) % 10
	if check != int(d[12]-'0') {
		return false, "ISBN-13 checksum mismatch"
	}
	return true, ""
}

// ValidateURL checks that the value parses as an absolute URL with both
// a scheme and a host. Relative URLs and bare hostnames are rejected.
func ValidateURL(raw string) (bool, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false, "empty"
	}
	u, err := url.Parse(s)
	if err != nil {
		return false, "parse error"
	}
	if u.Scheme == "" {
		return false, "missing scheme"
	}
	if u.Host == "" {
		return false, "missing host"
	}
	return true, ""
}

// dateRegex matches Zotero's sortable date prefix: YYYY, YYYY-MM, or
// YYYY-MM-DD. The regex alone doesn't catch month/day ranges — we do
// that with additional parsing in ValidateDate.
var dateRegex = regexp.MustCompile(`^(\d{4})(?:-(\d{1,2})(?:-(\d{1,2}))?)?$`)

// ValidateDate checks the first whitespace-delimited token of the value,
// matching Zotero's "YYYY-MM-DD originalText" dual-encoding. The year
// must fall within [1000, 2500]; month is [1, 12]; day is validated with
// precise Gregorian leap-year handling for February.
//
// Zotero pads unspecified components with "00" — e.g. a year-only entry
// is stored as "1871-00-00 1871". Both month and day accept 0 as the
// "unspecified" marker so the validator reflects Zotero's own semantics.
func ValidateDate(raw string) (bool, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false, "empty"
	}
	// Grab the first token — Zotero dual-encodes dates.
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	m := dateRegex.FindStringSubmatch(s)
	if m == nil {
		return false, "not YYYY[-MM[-DD]]"
	}
	year, _ := strconv.Atoi(m[1])
	if year < 1000 || year > 2500 {
		return false, "year out of [1000, 2500]"
	}
	if m[2] != "" {
		month, _ := strconv.Atoi(m[2])
		if month > 12 {
			return false, "month out of [1, 12]"
		}
		if m[3] != "" && month > 0 {
			day, _ := strconv.Atoi(m[3])
			if day > daysInMonth(year, month) {
				return false, "day out of range"
			}
		}
	}
	return true, ""
}

// daysInMonth returns the last valid day for the given Gregorian month.
// February is 29 on leap years, 28 otherwise.
func daysInMonth(year, month int) int {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if isLeapYear(year) {
			return 29
		}
		return 28
	}
	return 0
}

// isLeapYear applies the Gregorian rule: divisible by 4, except centuries
// that aren't divisible by 400.
func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
