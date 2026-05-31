package duck

// results.go — typed return values for each verb. All implement
// [cmdutil.Result] (JSON() any, Human() string) duck-typed so the
// duck package does not import cmdutil and stays leaf-level.
//
// JSON() returns the struct itself (the typed payload). Human() renders
// that same structured data through the shared uikit table renderer at
// call time — see render.go — so terminal-width truncation reflects the
// live terminal rather than a string frozen at construction.

// ColumnInfo is one row of the resolved schema: column name, the duckdb
// type used to read the column (post-promotion), and — for SQLite sources —
// the column's declared type plus the count of non-empty cells that
// failed to cast cleanly to it (non-zero means we fell back to VARCHAR
// to preserve those cells verbatim).
type ColumnInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Declared     string `json:"declared,omitempty"`
	FailingCells int    `json:"failing_cells,omitempty"`
}

// ColsResult is the result of [Cols].
type ColsResult struct {
	Path    string       `json:"path"`
	Table   string       `json:"table,omitempty"`
	Columns []ColumnInfo `json:"columns"`
}

// JSON satisfies cmdutil.Result.
func (r *ColsResult) JSON() any { return r }

// RowsResult is the result of [Head], [Tail], and [Query].
type RowsResult struct {
	Path    string           `json:"path"`
	Table   string           `json:"table,omitempty"`
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

func (r *RowsResult) JSON() any { return r }

// GlimpseColumn is one row of a transposed glimpse view: a column with
// its type and the first N sample values from the file.
type GlimpseColumn struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Samples []any  `json:"samples"`
}

// GlimpseResult is the result of [Glimpse].
type GlimpseResult struct {
	Path     string          `json:"path"`
	Table    string          `json:"table,omitempty"`
	RowCount int             `json:"row_count"`
	Columns  []GlimpseColumn `json:"columns"`
}

func (r *GlimpseResult) JSON() any { return r }

// ShapeResult is the result of [Shape].
type ShapeResult struct {
	Path    string `json:"path"`
	Table   string `json:"table,omitempty"`
	Rows    int    `json:"rows"`
	Columns int    `json:"columns"`
}

func (r *ShapeResult) JSON() any { return r }

// SummarizeColumn is a per-column row of the SUMMARIZE table. Numeric
// fields stay as strings to preserve duckdb's full precision.
type SummarizeColumn struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	Min            string `json:"min,omitempty"`
	Max            string `json:"max,omitempty"`
	ApproxUnique   int    `json:"approx_unique"`
	Avg            string `json:"avg,omitempty"`
	Std            string `json:"std,omitempty"`
	Q25            string `json:"q25,omitempty"`
	Q50            string `json:"q50,omitempty"`
	Q75            string `json:"q75,omitempty"`
	Count          int    `json:"count"`
	NullPercentage string `json:"null_percentage,omitempty"`
}

// SummarizeResult is the result of [Summarize].
type SummarizeResult struct {
	Path    string            `json:"path"`
	Table   string            `json:"table,omitempty"`
	Columns []SummarizeColumn `json:"columns"`
}

func (r *SummarizeResult) JSON() any { return r }

// ConvertResult is the result of [Convert].
type ConvertResult struct {
	Input     string `json:"input"`
	Output    string `json:"output"`
	Rows      int    `json:"rows"`
	humanText string
}

func (r *ConvertResult) JSON() any     { return r }
func (r *ConvertResult) Human() string { return r.humanText }
