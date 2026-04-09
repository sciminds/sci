package share

import (
	"strings"
	"testing"
)

// ---- CloudResult ----

func TestCloudResult_JSON(t *testing.T) {
	r := CloudResult{OK: true, Action: "share", Message: "shared successfully"}
	got, ok := r.JSON().(CloudResult)
	if !ok {
		t.Fatal("JSON() did not return CloudResult")
	}
	if got.Action != "share" || got.Message != "shared successfully" {
		t.Errorf("JSON() = %+v, unexpected values", got)
	}
}

func TestCloudResult_JSON_WithURL(t *testing.T) {
	r := CloudResult{OK: true, Action: "share", Message: "shared \"iris.csv\"", URL: "https://pub-xxx.r2.dev/user/iris.csv"}
	got, ok := r.JSON().(CloudResult)
	if !ok {
		t.Fatal("JSON() did not return CloudResult")
	}
	if got.URL != "https://pub-xxx.r2.dev/user/iris.csv" {
		t.Errorf("JSON() URL = %q", got.URL)
	}
}

func TestCloudResult_Human(t *testing.T) {
	tests := []struct {
		name       string
		r          CloudResult
		wantSymbol string
	}{
		{"ok", CloudResult{OK: true, Action: "share", Message: "shared"}, "✓"},
		{"fail", CloudResult{OK: false, Action: "get", Message: "auth failed"}, "✗"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.r.Human()
			if h == "" {
				t.Fatal("Human() returned empty string")
			}
			if !strings.Contains(h, tt.r.Message) {
				t.Errorf("Human() missing message %q:\n%s", tt.r.Message, h)
			}
			if !strings.Contains(h, tt.wantSymbol) {
				t.Errorf("Human() missing symbol %q:\n%s", tt.wantSymbol, h)
			}
		})
	}
}

func TestCloudResult_Human_WithURL(t *testing.T) {
	r := CloudResult{OK: true, Action: "share", Message: "shared \"iris.csv\"", URL: "https://pub-xxx.r2.dev/user/iris.csv"}
	h := r.Human()
	if !strings.Contains(h, "https://pub-xxx.r2.dev/user/iris.csv") {
		t.Errorf("Human() missing URL:\n%s", h)
	}
	if !strings.Contains(h, "sci cloud get <name>") {
		t.Errorf("Human() missing get hint:\n%s", h)
	}
}

func TestCloudResult_Human_WithoutURL(t *testing.T) {
	r := CloudResult{OK: true, Action: "get", Message: "downloaded"}
	h := r.Human()
	if strings.Contains(h, "sci get") {
		t.Errorf("Human() should not show get hint for non-share actions:\n%s", h)
	}
}

// ---- DatasetListResult ----

func TestDatasetListResult_JSON(t *testing.T) {
	r := DatasetListResult{Datasets: []DatasetListEntry{
		{Name: "iris.csv", Owner: "alice", Type: "csv", URL: "https://pub-xxx.r2.dev/alice/iris.csv"},
	}}
	got, ok := r.JSON().(DatasetListResult)
	if !ok {
		t.Fatal("JSON() did not return DatasetListResult")
	}
	if len(got.Datasets) != 1 || got.Datasets[0].Name != "iris.csv" {
		t.Errorf("JSON() = %+v", got)
	}
}

func TestDatasetListResult_Human(t *testing.T) {
	r := DatasetListResult{Datasets: []DatasetListEntry{
		{Name: "iris.csv", Owner: "alice", Type: "csv", Updated: "2024-01-01", URL: "https://example.com/alice/iris.csv"},
		{Name: "titanic.csv", Owner: "bob", Type: "csv", Updated: "2024-02-01", URL: "https://example.com/bob/titanic.csv"},
	}}
	h := r.Human()
	if h == "" {
		t.Fatal("Human() returned empty string")
	}
	for _, want := range []string{"iris.csv", "titanic.csv", "csv", "alice", "bob"} {
		if !strings.Contains(h, want) {
			t.Errorf("Human() missing %q:\n%s", want, h)
		}
	}
}

func TestDatasetListResult_Human_Empty(t *testing.T) {
	r := DatasetListResult{Datasets: nil}
	h := r.Human()
	if !strings.Contains(h, "no files") {
		t.Errorf("Human() should say no files when empty:\n%s", h)
	}
}

// ---- AuthResult ----

func TestAuthResult_JSON(t *testing.T) {
	r := AuthResult{OK: true, Action: "login", Username: "alice", Message: "configured as alice"}
	got, ok := r.JSON().(AuthResult)
	if !ok {
		t.Fatal("JSON() did not return AuthResult")
	}
	if got.Username != "alice" || got.Action != "login" {
		t.Errorf("JSON() = %+v, unexpected values", got)
	}
}

func TestAuthResult_Human(t *testing.T) {
	tests := []struct {
		name          string
		r             AuthResult
		wantSymbol    string
		wantNextSteps bool
	}{
		{
			name:          "login ok — shows next steps",
			r:             AuthResult{OK: true, Action: "login", Username: "alice", Message: "configured as alice"},
			wantSymbol:    "✓",
			wantNextSteps: true,
		},
		{
			name:          "status ok — shows next steps",
			r:             AuthResult{OK: true, Action: "status", Username: "alice", Message: "configured as alice"},
			wantSymbol:    "✓",
			wantNextSteps: true,
		},
		{
			name:          "logout — no next steps",
			r:             AuthResult{OK: true, Action: "logout", Message: "credentials removed"},
			wantSymbol:    "✓",
			wantNextSteps: false,
		},
		{
			name:          "other action — no next steps",
			r:             AuthResult{OK: false, Action: "register", Message: "error"},
			wantSymbol:    "✗",
			wantNextSteps: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.r.Human()
			if h == "" {
				t.Fatal("Human() returned empty string")
			}
			if !strings.Contains(h, tt.r.Message) {
				t.Errorf("Human() missing message %q:\n%s", tt.r.Message, h)
			}
			if !strings.Contains(h, tt.wantSymbol) {
				t.Errorf("Human() missing symbol %q:\n%s", tt.wantSymbol, h)
			}
			hasNextSteps := strings.Contains(h, "sci cloud share") && strings.Contains(h, "sci cloud list")
			if hasNextSteps != tt.wantNextSteps {
				t.Errorf("Human() next-steps present=%v, want %v:\n%s", hasNextSteps, tt.wantNextSteps, h)
			}
		})
	}
}

// ---- SharedListResult ----

func TestSharedListResult_JSON(t *testing.T) {
	r := SharedListResult{Datasets: []SharedEntry{
		{Name: "iris.csv", Type: "csv", Updated: "2024-01-01", URL: "https://example.com/user/iris.csv", Size: 1024},
	}}
	got, ok := r.JSON().(SharedListResult)
	if !ok {
		t.Fatal("JSON() did not return SharedListResult")
	}
	if len(got.Datasets) != 1 || got.Datasets[0].Name != "iris.csv" {
		t.Errorf("JSON() = %+v", got)
	}
}

func TestSharedListResult_Human(t *testing.T) {
	r := SharedListResult{Datasets: []SharedEntry{
		{Name: "iris.csv", Type: "csv", Updated: "2024-01-01", URL: "https://example.com/user/iris.csv", Size: 2048},
		{Name: "penguins.csv", Type: "csv", Updated: "2024-03-10", URL: "https://example.com/user/penguins.csv", Size: 4096},
	}}
	h := r.Human()
	if h == "" {
		t.Fatal("Human() returned empty string")
	}
	for _, want := range []string{"iris.csv", "penguins.csv", "csv"} {
		if !strings.Contains(h, want) {
			t.Errorf("Human() missing %q:\n%s", want, h)
		}
	}
}

func TestSharedListResult_Human_Empty(t *testing.T) {
	r := SharedListResult{Datasets: nil}
	h := r.Human()
	if !strings.Contains(h, "no files") {
		t.Errorf("Human() should say 'no files' when empty:\n%s", h)
	}
}

func TestSharedListResult_Human_ShowsOwner(t *testing.T) {
	r := SharedListResult{Datasets: []SharedEntry{
		{Name: "iris.csv", Owner: "alice", Type: "csv", Updated: "2024-01-01", URL: "https://pub.r2.dev/alice/iris.csv", Size: 1024},
		{Name: "penguins.csv", Owner: "bob", Type: "csv", Updated: "2024-02-01", URL: "https://pub.r2.dev/bob/penguins.csv", Size: 2048},
	}}
	h := r.Human()
	if !strings.Contains(h, "alice") {
		t.Errorf("Human() missing owner 'alice':\n%s", h)
	}
	if !strings.Contains(h, "bob") {
		t.Errorf("Human() missing owner 'bob':\n%s", h)
	}
	// Should show the owner column header.
	if !strings.Contains(h, "owner") {
		t.Errorf("Human() missing 'owner' column header:\n%s", h)
	}
}

func TestSharedListResult_Human_NoOwnerColumn_WhenEmpty(t *testing.T) {
	r := SharedListResult{Datasets: []SharedEntry{
		{Name: "iris.csv", Type: "csv", Updated: "2024-01-01", URL: "https://pub.r2.dev/user/iris.csv", Size: 1024},
	}}
	h := r.Human()
	// When Owner is empty on all entries, should NOT show owner column.
	if strings.Contains(h, "owner") {
		t.Errorf("Human() should not show 'owner' column when no entries have Owner set:\n%s", h)
	}
}

func TestSharedListResult_Human_ShowsURL(t *testing.T) {
	r := SharedListResult{Datasets: []SharedEntry{
		{Name: "iris.csv", Type: "csv", Updated: "2024-01-01", URL: "https://pub-xxx.r2.dev/user/iris.csv", Size: 512},
	}}
	h := r.Human()
	if !strings.Contains(h, "https://pub-xxx.r2.dev/user/iris.csv") {
		t.Errorf("Human() should show URL:\n%s", h)
	}
}
