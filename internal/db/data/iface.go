package data

// iface.go — type aliases forwarding [github.com/sciminds/cli/internal/tui/dbtui/data] types
// so that callers can use data.DataStore without importing the dbtui package directly.

import dbtuistore "github.com/sciminds/cli/internal/tui/dbtui/data"

// Type aliases forwarding core datastore types.

// DataStore is the interface for all database-backed stores.
type DataStore = dbtuistore.DataStore //nolint:revive // name is established in the API

// PragmaColumn describes a column from SQLite's PRAGMA table_info.
type PragmaColumn = dbtuistore.PragmaColumn

// TableSummary holds a table's name, row count, and column count.
type TableSummary = dbtuistore.TableSummary

// RowIdentifier locates a row by rowid or composite PK.
type RowIdentifier = dbtuistore.RowIdentifier

// MaxTableRows caps the number of rows loaded per table.
const MaxTableRows = dbtuistore.MaxTableRows

// ValidateReadOnlySQL checks that a SQL string is a safe SELECT.
var ValidateReadOnlySQL = dbtuistore.ValidateReadOnlySQL

// IsSafeIdentifier forwards to the dbtui implementation.
var IsSafeIdentifier = dbtuistore.IsSafeIdentifier

// ContainsWriteKeyword checks for write keywords in SQL.
var ContainsWriteKeyword = dbtuistore.ContainsWriteKeyword

// maxQueryRows caps the number of rows returned by ReadOnlyQuery.
const maxQueryRows = 200
