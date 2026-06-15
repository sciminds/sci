package duck

import "testing"

func TestValidDuckType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		// shapes DuckDB's sqlite_scanner actually emits
		{"BIGINT", true},
		{"INTEGER", true},
		{"VARCHAR", true},
		{"DOUBLE", true},
		{"BOOLEAN", true},
		{"BLOB", true},
		{"DATE", true},
		{"TIMESTAMP", true},
		{"DECIMAL(18,3)", true},
		{"VARCHAR(100)", true},
		{"DECIMAL(18, 3)", true},

		// reject anything that could carry SQL injection
		{"INTEGER) UNION SELECT 1 FROM users--", false},
		{"INTEGER; DROP TABLE x", false},
		{`INTEGER" AS x, "y`, false},
		{"INTEGER) AS pwn, COUNT(*", false},
		{"integer", false}, // DuckDB normalizes to uppercase
		{"", false},
		{"STRUCT(a INTEGER)", false}, // nested types intentionally rejected
		{"LIST(BIGINT)", false},
		{"--", false},
		{"/* comment */ BIGINT", false},
	}
	for _, c := range cases {
		got := validDuckType.MatchString(c.in)
		if got != c.want {
			t.Errorf("validDuckType.MatchString(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
