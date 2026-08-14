package local

import (
	"slices"
	"testing"
)

func TestSearchFulltext_SingleWord(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	ids, err := db.SearchFulltext([]string{"neuroimaging"}, false)
	if err != nil {
		t.Fatal(err)
	}
	// "neuroimaging" is only on attachment 40 → parent 10.
	if !slices.Equal(ids, []int64{10}) {
		t.Errorf("got %v, want [10]", ids)
	}
}

func TestSearchFulltext_MultiWord(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// Both "brain" and "network" → only attachment 40 (parent 10) has both.
	ids, err := db.SearchFulltext([]string{"brain", "network"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []int64{10}) {
		t.Errorf("got %v, want [10]", ids)
	}
}

func TestSearchFulltext_SharedWord(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// "brain" appears on both attachments 40 (parent 10) and 81 (parent 80).
	ids, err := db.SearchFulltext([]string{"brain"}, false)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []int64{10, 80}) {
		t.Errorf("got %v, want [10 80]", ids)
	}
}

func TestSearchFulltext_Prefix(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// "neuro" should prefix-match "neuroimaging" → parent 10.
	ids, err := db.SearchFulltext([]string{"neuro"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []int64{10}) {
		t.Errorf("got %v, want [10]", ids)
	}
}

func TestSearchFulltext_Exact(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// "neuro" with exact=true should NOT match "neuroimaging".
	ids, err := db.SearchFulltext([]string{"neuro"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("exact match for 'neuro' got %v, want empty", ids)
	}

	// "brain" with exact=true should match exactly.
	ids, err = db.SearchFulltext([]string{"brain"}, true)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []int64{10, 80}) {
		t.Errorf("exact match for 'brain' got %v, want [10 80]", ids)
	}
}

func TestSearchFulltext_CaseInsensitive(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	// "Brain" (uppercase) should match "brain" (stored lowercase).
	ids, err := db.SearchFulltext([]string{"Brain"}, false)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []int64{10, 80}) {
		t.Errorf("got %v, want [10 80]", ids)
	}
}

func TestSearchFulltext_NoMatch(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	ids, err := db.SearchFulltext([]string{"xyznonexistent"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("got %v, want empty", ids)
	}
}

func TestSearchFulltext_EmptyInput(t *testing.T) {
	t.Parallel()
	db := openFixture(t)

	ids, err := db.SearchFulltext(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if ids != nil {
		t.Errorf("got %v, want nil", ids)
	}

	ids, err = db.SearchFulltext([]string{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if ids != nil {
		t.Errorf("got %v, want nil", ids)
	}
}
