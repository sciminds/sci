package db

import (
	"strings"
	"testing"
)

// ---- InfoResult ----

func TestInfoResult_JSON(t *testing.T) {
	r := InfoResult{
		SizeMB: 1.5,
		Tables: []TableEntry{
			{Name: "users", Rows: 10, Columns: 4},
			{Name: "orders", Rows: 50, Columns: 3},
		},
	}
	got, ok := r.JSON().(InfoResult)
	if !ok {
		t.Fatal("JSON() did not return InfoResult")
	}
	if got.SizeMB != 1.5 || len(got.Tables) != 2 {
		t.Errorf("JSON() = %+v, unexpected values", got)
	}
	if got.Tables[0].Name != "users" {
		t.Errorf("JSON() Tables[0].Name = %q, want %q", got.Tables[0].Name, "users")
	}
}

func TestInfoResult_Human(t *testing.T) {
	r := InfoResult{
		SizeMB: 2.25,
		Tables: []TableEntry{
			{Name: "users", Rows: 100, Columns: 5},
			{Name: "orders", Rows: 42, Columns: 3},
		},
	}
	h := r.Human()
	if h == "" {
		t.Fatal("Human() returned empty string")
	}
	for _, want := range []string{"2.25", "users", "100", "orders", "42"} {
		if !strings.Contains(h, want) {
			t.Errorf("Human() missing %q:\n%s", want, h)
		}
	}
}

func TestInfoResult_Human_Empty(t *testing.T) {
	r := InfoResult{SizeMB: 0, Tables: nil}
	h := r.Human()
	if h == "" {
		t.Fatal("Human() returned empty string for empty database")
	}
}

// ---- TablesResult ----

func TestTablesResult_JSON(t *testing.T) {
	r := TablesResult{Tables: []TableEntry{
		{Name: "users", Rows: 10, Columns: 4},
	}}
	got, ok := r.JSON().(TablesResult)
	if !ok {
		t.Fatal("JSON() did not return TablesResult")
	}
	if len(got.Tables) != 1 || got.Tables[0].Name != "users" {
		t.Errorf("JSON() = %+v, unexpected values", got)
	}
}

func TestTablesResult_Human(t *testing.T) {
	r := TablesResult{Tables: []TableEntry{
		{Name: "users", Rows: 100, Columns: 5},
		{Name: "orders", Rows: 42, Columns: 3},
	}}
	h := r.Human()
	if h == "" {
		t.Fatal("Human() returned empty string")
	}
	for _, want := range []string{"users", "100", "5", "orders", "42", "3"} {
		if !strings.Contains(h, want) {
			t.Errorf("Human() missing %q:\n%s", want, h)
		}
	}
}

func TestTablesResult_Human_Empty(t *testing.T) {
	r := TablesResult{Tables: nil}
	h := r.Human()
	if h == "" {
		t.Fatal("Human() returned empty string for empty table list")
	}
}

// ---- MutationResult ----

func TestMutationResult_JSON(t *testing.T) {
	r := MutationResult{OK: true, Message: "table created"}
	got, ok := r.JSON().(MutationResult)
	if !ok {
		t.Fatal("JSON() did not return MutationResult")
	}
	if !got.OK || got.Message != "table created" {
		t.Errorf("JSON() = %+v, unexpected values", got)
	}
}

func TestMutationResult_Human(t *testing.T) {
	tests := []struct {
		name        string
		r           MutationResult
		wantContain string
		wantSymbol  string
	}{
		{
			name:        "success",
			r:           MutationResult{OK: true, Message: "row added to users in mydb"},
			wantContain: "row added to users in mydb",
			wantSymbol:  "✓",
		},
		{
			name:        "failure",
			r:           MutationResult{OK: false, Message: "table not found"},
			wantContain: "table not found",
			wantSymbol:  "✗",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.r.Human()
			if h == "" {
				t.Fatal("Human() returned empty string")
			}
			if !strings.Contains(h, tt.wantContain) {
				t.Errorf("Human() missing %q:\n%s", tt.wantContain, h)
			}
			if !strings.Contains(h, tt.wantSymbol) {
				t.Errorf("Human() missing symbol %q:\n%s", tt.wantSymbol, h)
			}
		})
	}
}
