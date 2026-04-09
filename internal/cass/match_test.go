package cass

import "testing"

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Alice Chen", "alice chen"},
		{"Smith, John", "john smith"},
		{"  Bob   Park  ", "bob park"},
		{"john.smith@example.com", "johnsmithexamplecom"},
		{"O'Brien, Mary-Jane", "maryjane obrien"},
		{"ALICE CHEN", "alice chen"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeName(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Lab 1: Data Viz", "lab-1-data-viz"},
		{"Final Project", "final-project"},
		{"already-a-slug", "already-a-slug"},
		{"  spaces   everywhere  ", "spaces-everywhere"},
		{"Special (chars) / here", "special-chars-here"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Slugify(tt.input)
			if got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFindCandidates(t *testing.T) {
	canvas := []Student{
		{CanvasID: 1, Name: "Alice Chen"},
		{CanvasID: 2, Name: "Bob Park"},
		{CanvasID: 3, Name: "Alice Cheng"},
		{CanvasID: 4, Name: "Carol Davis"},
	}

	t.Run("exact match", func(t *testing.T) {
		candidates := FindCandidates("Alice Chen", canvas)
		if len(candidates) == 0 {
			t.Fatal("expected candidates")
		}
		if candidates[0].Student.CanvasID != 1 {
			t.Errorf("best match = %d, want 1", candidates[0].Student.CanvasID)
		}
		if candidates[0].Score != 100 {
			t.Errorf("score = %d, want 100 for exact match", candidates[0].Score)
		}
	})

	t.Run("fuzzy match", func(t *testing.T) {
		// GitHub names often have first+last tokens.
		candidates := FindCandidates("Alice C", canvas)
		if len(candidates) == 0 {
			t.Fatal("expected candidates")
		}
		// Alice Chen and Alice Cheng should both match on "alice" token.
		if len(candidates) < 2 {
			t.Errorf("expected at least 2 candidates, got %d", len(candidates))
		}
	})

	t.Run("no match", func(t *testing.T) {
		candidates := FindCandidates("xyz-unknown", canvas)
		if len(candidates) != 0 {
			t.Errorf("expected no candidates, got %d", len(candidates))
		}
	})
}

func TestAutoMatch(t *testing.T) {
	canvas := []Student{
		{CanvasID: 1, Name: "Alice Chen"},
		{CanvasID: 2, Name: "Bob Park"},
		{CanvasID: 3, Name: "Carol Davis"},
	}

	ghNames := []string{"Alice Chen", "Bob Park", "Unknown User"}

	matched, unmatched := AutoMatch(ghNames, canvas)

	if len(matched) != 2 {
		t.Errorf("matched = %d, want 2", len(matched))
	}
	if len(unmatched) != 1 {
		t.Errorf("unmatched = %d, want 1", len(unmatched))
	}
	if unmatched[0] != "Unknown User" {
		t.Errorf("unmatched[0] = %q", unmatched[0])
	}

	// Verify matches are correct.
	for _, m := range matched {
		if m.GHName == "Alice Chen" && m.Student.CanvasID != 1 {
			t.Errorf("Alice matched to %d, want 1", m.Student.CanvasID)
		}
		if m.GHName == "Bob Park" && m.Student.CanvasID != 2 {
			t.Errorf("Bob matched to %d, want 2", m.Student.CanvasID)
		}
	}
}

func TestAutoMatch_AllAlreadyMatched(t *testing.T) {
	// When all students are already matched, should return empty.
	canvas := []Student{
		{CanvasID: 1, Name: "Alice Chen"},
	}
	matched, unmatched := AutoMatch(nil, canvas)
	if len(matched) != 0 {
		t.Errorf("matched = %d", len(matched))
	}
	if len(unmatched) != 0 {
		t.Errorf("unmatched = %d", len(unmatched))
	}
}

func TestAutoMatch_DuplicateCanvasStudent(t *testing.T) {
	// Each Canvas student should only match once.
	canvas := []Student{
		{CanvasID: 1, Name: "Alice Chen"},
	}
	ghNames := []string{"Alice Chen", "Chen Alice"} // both normalize to same

	matched, unmatched := AutoMatch(ghNames, canvas)
	if len(matched) != 1 {
		t.Errorf("matched = %d, want 1 (each Canvas student matches at most once)", len(matched))
	}
	if len(unmatched) != 1 {
		t.Errorf("unmatched = %d, want 1", len(unmatched))
	}
}

func TestScoreName_BoundaryConditions(t *testing.T) {
	// Exactly 50% overlap.
	score := scoreName("alice bob", []string{"alice", "bob"}, "alice charlie")
	if score == 0 {
		t.Error("expected non-zero score for 50% overlap (alice matches)")
	}

	// Below 50% overlap.
	score = scoreName("alice bob charlie", []string{"alice", "bob", "charlie"}, "alice dave eve frank")
	if score != 0 {
		t.Errorf("expected 0 for < 50%% overlap, got %d", score)
	}

	// Empty after normalization.
	score = scoreName("", nil, "alice")
	if score != 0 {
		t.Errorf("expected 0 for empty input, got %d", score)
	}
}

func TestAutoMatch_LastFirst(t *testing.T) {
	canvas := []Student{
		{CanvasID: 1, Name: "Smith, John"},
	}
	ghNames := []string{"John Smith"}

	matched, unmatched := AutoMatch(ghNames, canvas)
	if len(matched) != 1 {
		t.Fatalf("matched = %d, want 1", len(matched))
	}
	if len(unmatched) != 0 {
		t.Errorf("unmatched = %d, want 0", len(unmatched))
	}
}
