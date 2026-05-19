// Package duck implements [store.DataStore] backed by a long-running
// `duckdb` CLI subprocess.
//
// dbtui is interface-bound, so the same model can drive either backend.
// The subprocess runs read-write; row-level mutations (UpdateCell,
// DeleteRows) additionally require the target table to have a PRIMARY
// KEY because DuckDB has no implicit rowid. Tables without a PK return
// false from [Store.IsRowEditable] and surface as read-only tabs in
// dbtui.
//
// Each query is framed by a unique sentinel SELECT that follows the
// caller's SQL on stdin. The reader walks stdout lines (each a JSON
// object — one row of result) until it sees the sentinel, then returns
// the accumulated rows. Errors land on stderr and are attached to the
// next sentinel hand-off.
//
// The duckdb binary is required: [Open] returns [duck.ErrNotInstalled]
// (the existing one-shot package's error) when it is missing so callers
// can route the user to `sci doctor`.
package duck
