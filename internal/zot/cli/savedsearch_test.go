package cli

import (
	"strings"
	"testing"

	"github.com/sciminds/cli/internal/zot/client"
)

func TestParseConditionSpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    client.SearchCondition
		wantErr bool
	}{
		{
			in:   "title:contains:transformer",
			want: client.SearchCondition{Condition: "title", Operator: "contains", Value: "transformer"},
		},
		{
			in:   "tag:is:lit-review",
			want: client.SearchCondition{Condition: "tag", Operator: "is", Value: "lit-review"},
		},
		{
			// Value contains a colon → only first two splits matter.
			in:   "fulltextContent:contains:see https://example.com",
			want: client.SearchCondition{Condition: "fulltextContent", Operator: "contains", Value: "see https://example.com"},
		},
		{
			in:   "dateAdded:isInTheLast:30 days",
			want: client.SearchCondition{Condition: "dateAdded", Operator: "isInTheLast", Value: "30 days"},
		},
		{
			// Empty value is allowed for pseudo-conditions.
			in:   "noChildren:true:",
			want: client.SearchCondition{Condition: "noChildren", Operator: "true", Value: ""},
		},
		{in: "title:contains", wantErr: true},
		{in: "no-colon-at-all", wantErr: true},
		{in: ":contains:foo", wantErr: true},
		{in: "title::foo", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parseConditionSpec(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: want error, got %+v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q:\n  got  %+v\n  want %+v", tc.in, got, tc.want)
		}
	}
}

func TestBuildSavedSearchConditions_FlagsOnly(t *testing.T) {
	t.Parallel()
	conds, err := buildSavedSearchConditions(
		[]string{"title:contains:foo", "tag:is:bar"},
		"",
		false,
		strings.NewReader(""),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(conds) != 2 {
		t.Fatalf("len = %d, want 2", len(conds))
	}
	if conds[0].Condition != "title" || conds[1].Condition != "tag" {
		t.Errorf("ordering off: %+v", conds)
	}
}

func TestBuildSavedSearchConditions_AnyPrependsJoinMode(t *testing.T) {
	t.Parallel()
	conds, err := buildSavedSearchConditions(
		[]string{"title:contains:foo", "tag:is:bar"},
		"",
		true, // --any
		strings.NewReader(""),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(conds) != 3 {
		t.Fatalf("len = %d, want 3 (joinMode prepended)", len(conds))
	}
	if conds[0].Condition != "joinMode" || conds[0].Operator != "any" {
		t.Errorf("first condition = %+v, want joinMode/any", conds[0])
	}
}

func TestBuildSavedSearchConditions_FromJSON(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader(`[
		{"condition":"title","operator":"contains","value":"foo"},
		{"condition":"itemType","operator":"is","value":"journalArticle"}
	]`)
	conds, err := buildSavedSearchConditions(nil, "-", false, stdin)
	if err != nil {
		t.Fatal(err)
	}
	if len(conds) != 2 {
		t.Fatalf("len = %d, want 2", len(conds))
	}
	if conds[0].Value != "foo" || conds[1].Value != "journalArticle" {
		t.Errorf("payload mismatch: %+v", conds)
	}
}

func TestBuildSavedSearchConditions_RejectsBoth(t *testing.T) {
	t.Parallel()
	_, err := buildSavedSearchConditions(
		[]string{"title:contains:foo"},
		"-",
		false,
		strings.NewReader(`[]`),
	)
	if err == nil {
		t.Fatal("expected mutex error")
	}
}

func TestBuildSavedSearchConditions_RejectsNeither(t *testing.T) {
	t.Parallel()
	_, err := buildSavedSearchConditions(nil, "", false, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error when no conditions provided")
	}
}

func TestBuildSavedSearchConditions_FromJSONValidation(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader(`[{"condition":"","operator":"is","value":"x"}]`)
	_, err := buildSavedSearchConditions(nil, "-", false, stdin)
	if err == nil {
		t.Fatal("expected validation error")
	}
}
