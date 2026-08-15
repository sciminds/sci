package main

import (
	"errors"
	"testing"

	"github.com/sciminds/sci/internal/cmdutil"
	"github.com/sciminds/sci/internal/store"
)

// A refused query is a malformed command line, not a failed unit of work:
// `sci db query data.csv "DROP TABLE src"` must code usage and exit 2.
//
// The check lives here rather than beside ValidateReadOnlySQL because that
// validator deliberately returns a bare sentinel — it also guards dbtui's
// query pane, and coding the error inside internal/store would drag 37
// terminal-UI packages into a storage layer. wrapDuckErr is where the
// sentinel becomes a CLI answer, so this is where the contract is real.
func TestWrapDuckErr_RefusedQueryCodesUsage(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"write statement":     "DROP TABLE src",
		"multiple statements": "SELECT 1; SELECT 2",
		"writable CTE":        "WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x",
		"empty":               "   ",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := store.ValidateReadOnlySQL(query)
			if err == nil {
				t.Fatalf("ValidateReadOnlySQL(%q) accepted the query", query)
			}
			coded, ok := errors.AsType[*cmdutil.CodedError](wrapDuckErr(err))
			if !ok {
				t.Fatalf("wrapDuckErr(%q) carries no cmdutil.CodedError", err)
			}
			if coded.Code != cmdutil.CodeUsage {
				t.Errorf("code = %q, want %q", coded.Code, cmdutil.CodeUsage)
			}
			if coded.Try == "" && coded.Fix == "" {
				t.Errorf("no fix/try on %q", coded.Message)
			}
		})
	}
}
