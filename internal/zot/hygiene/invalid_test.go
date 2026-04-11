package hygiene

import (
	"os"
	"testing"

	"github.com/sciminds/cli/internal/zot/local"
)

func TestInvalid_FromFieldValues(t *testing.T) {
	// Bypass the DB entirely — exercise the orchestrator over a
	// hand-built slice of FieldValue rows covering all four validators
	// with both valid and invalid cases.
	rows := []local.FieldValue{
		{Key: "A1", Title: "good doi", Field: "DOI", Value: "10.1000/abc"},
		{Key: "A2", Title: "bad doi", Field: "DOI", Value: "not-a-doi"},
		{Key: "B1", Title: "good isbn", Field: "ISBN", Value: "978-0-306-40615-7"},
		{Key: "B2", Title: "bad isbn", Field: "ISBN", Value: "9780306406158"},
		{Key: "C1", Title: "good url", Field: "url", Value: "https://example.org"},
		{Key: "C2", Title: "bad url", Field: "url", Value: "example.org"},
		{Key: "D1", Title: "good date", Field: "date", Value: "2024-03-15"},
		{Key: "D2", Title: "bad date", Field: "date", Value: "March 2024"},
		{Key: "E1", Title: "unrelated", Field: "abstractNote", Value: "text"}, // ignored
	}
	rep := InvalidFromFieldValues(rows)
	if rep.Scanned != len(rows) {
		t.Errorf("Scanned = %d, want %d", rep.Scanned, len(rows))
	}
	// Expect exactly 4 bad findings, one per invalid row above.
	if len(rep.Findings) != 4 {
		t.Fatalf("got %d findings, want 4: %+v", len(rep.Findings), rep.Findings)
	}
	// All invalid findings must be SevWarn.
	for _, f := range rep.Findings {
		if f.Severity != SevWarn {
			t.Errorf("finding %s: severity = %v, want SevWarn", f.Kind, f.Severity)
		}
		if f.Check != "invalid" {
			t.Errorf("finding %s: check = %q, want invalid", f.Kind, f.Check)
		}
	}
	// Kinds should be the Zotero field names.
	byKind := map[string]int{}
	for _, f := range rep.Findings {
		byKind[f.Kind]++
	}
	for _, k := range []string{"DOI", "ISBN", "url", "date"} {
		if byKind[k] != 1 {
			t.Errorf("kind %q count = %d, want 1", k, byKind[k])
		}
	}
}

func TestInvalid_RealLibrary(t *testing.T) {
	// SLOW-gated for parity with the other real-library tests. The
	// orchestrator is already covered by TestInvalid_FromFieldValues;
	// this one exists to eyeball bad-value counts on a real library.
	if testingSlow := testingSlowEnabled(); !testingSlow {
		t.Skip("set SLOW=1 to run real-library invalid scan")
	}
	db := openRealDB(t)
	rep, err := Invalid(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	stats, ok := rep.Stats.(InvalidStats)
	if !ok {
		t.Fatalf("stats is %T, want InvalidStats", rep.Stats)
	}
	t.Logf("scanned=%d findings=%d", rep.Scanned, len(rep.Findings))
	for _, c := range stats.PerField {
		t.Logf("  %-6s %5d / %-5d good  %5.1f%%  (%d bad)",
			c.Field, c.Scanned-c.Bad, c.Scanned, c.PercentGood, c.Bad)
	}
}

func testingSlowEnabled() bool {
	return os.Getenv("SLOW") != ""
}

func TestInvalid_IgnoresUnknownFields(t *testing.T) {
	// Fields outside our validator set must pass through without
	// error and without generating findings.
	rows := []local.FieldValue{
		{Key: "X1", Title: "t", Field: "publicationTitle", Value: "garbage"},
		{Key: "X2", Title: "t", Field: "language", Value: "???"},
	}
	rep := InvalidFromFieldValues(rows)
	if len(rep.Findings) != 0 {
		t.Errorf("unknown fields should not generate findings, got %+v", rep.Findings)
	}
}

func TestValidateDOI(t *testing.T) {
	valid := []string{
		"10.1000/abc123",
		"10.1016/j.conb.2014.07.014",
		"10.31234/osf.io/dp3ef",
		"10.1103/PhysRevLett.59.381",
		"10.1002/(SICI)1097-0258(19970130)16:2<139::AID-SIM474>3.0.CO;2-F", // classic gnarly DOI
	}
	for _, d := range valid {
		if ok, reason := ValidateDOI(d); !ok {
			t.Errorf("ValidateDOI(%q) rejected: %s", d, reason)
		}
	}
	invalid := []string{
		"",                    // empty
		"not-a-doi",           // no prefix
		"10.1/abc",            // prefix too short (<4 digits)
		"10.1000/",            // empty suffix
		"https://example.com", // URL, not DOI
		"10.1000 abc",         // space in suffix
	}
	for _, d := range invalid {
		if ok, _ := ValidateDOI(d); ok {
			t.Errorf("ValidateDOI(%q) accepted but should be invalid", d)
		}
	}
}

func TestValidateDOI_StripsPrefix(t *testing.T) {
	// Real-world DOIs are sometimes stored with a URL prefix. The
	// validator should accept them (Zotero itself does this).
	cases := []string{
		"https://doi.org/10.1000/abc",
		"http://dx.doi.org/10.1000/abc",
		"doi:10.1000/abc",
	}
	for _, d := range cases {
		if ok, reason := ValidateDOI(d); !ok {
			t.Errorf("ValidateDOI(%q) rejected: %s", d, reason)
		}
	}
}

func TestValidateISBN(t *testing.T) {
	valid := []string{
		"0306406152",    // ISBN-10, valid checksum
		"0-306-40615-2", // same, with hyphens
		"9780306406157", // ISBN-13, valid checksum
		"978-0-306-40615-7",
		"080442957X", // ISBN-10 ending in X
	}
	for _, s := range valid {
		if ok, reason := ValidateISBN(s); !ok {
			t.Errorf("ValidateISBN(%q) rejected: %s", s, reason)
		}
	}
	invalid := []string{
		"",
		"123",           // too short
		"0306406153",    // ISBN-10 bad checksum
		"9780306406158", // ISBN-13 bad checksum
		"abcdefghij",    // letters (no X rule)
	}
	for _, s := range invalid {
		if ok, _ := ValidateISBN(s); ok {
			t.Errorf("ValidateISBN(%q) accepted but should be invalid", s)
		}
	}
}

func TestValidateURL(t *testing.T) {
	valid := []string{
		"https://example.org",
		"http://example.org/path?q=1",
		"https://doi.org/10.1000/abc",
	}
	for _, u := range valid {
		if ok, reason := ValidateURL(u); !ok {
			t.Errorf("ValidateURL(%q) rejected: %s", u, reason)
		}
	}
	invalid := []string{
		"",
		"not a url",
		"example.org", // no scheme
		"ftp//broken", // malformed
		"://missing-scheme.org",
	}
	for _, u := range invalid {
		if ok, _ := ValidateURL(u); ok {
			t.Errorf("ValidateURL(%q) accepted but should be invalid", u)
		}
	}
}

func TestValidateDate(t *testing.T) {
	// Zotero stores dates as "YYYY-MM-DD originalText" — validator
	// should only look at the first token.
	valid := []string{
		"2024",
		"2024-03",
		"2024-03-15",
		"2024-03-15 March 15, 2024", // dual-encoding
		"1823",                      // historical but plausible
		"2024-02-29",                // leap year — 2024 is divisible by 4
		"2000-02-29",                // leap year — century divisible by 400
		"2500-12-31",                // upper bound inclusive
		"1871-00-00 1871",           // Zotero's year-only dual-encoding
		"2024-03-00",                // month only — day unspecified
	}
	for _, d := range valid {
		if ok, reason := ValidateDate(d); !ok {
			t.Errorf("ValidateDate(%q) rejected: %s", d, reason)
		}
	}
	invalid := []string{
		"",
		"March 2024",  // wrong shape
		"24-03-15",    // 2-digit year
		"20234-03-15", // 5-digit year
		"999",         // year too early
		"2501",        // year beyond upper bound
		"2024-13-01",  // month out of range
		"2024-02-30",  // day out of range
		"2023-02-29",  // not a leap year
		"1900-02-29",  // century not divisible by 400
		"2100-02-29",  // century not divisible by 400
	}
	for _, d := range invalid {
		if ok, _ := ValidateDate(d); ok {
			t.Errorf("ValidateDate(%q) accepted but should be invalid", d)
		}
	}
}
