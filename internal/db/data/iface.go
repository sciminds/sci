package data

// iface.go — type aliases forwarding [github.com/sciminds/cli/internal/tui/dbtui/data] types
// so that callers can use data.DataStore without importing the dbtui package directly.

import dbtuistore "github.com/sciminds/cli/internal/tui/dbtui/data"

// Type aliases forwarding core datastore types.

type DataStore = dbtuistore.DataStore
type PragmaColumn = dbtuistore.PragmaColumn
type TableSummary = dbtuistore.TableSummary
type RowIdentifier = dbtuistore.RowIdentifier

const MaxTableRows = dbtuistore.MaxTableRows

var ValidateReadOnlySQL = dbtuistore.ValidateReadOnlySQL

// IsSafeIdentifier forwards to the dbtui implementation.
var IsSafeIdentifier = dbtuistore.IsSafeIdentifier

var ContainsWriteKeyword = dbtuistore.ContainsWriteKeyword

// maxQueryRows caps the number of rows returned by ReadOnlyQuery.
const maxQueryRows = 200
