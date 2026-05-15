package duck

// results.go — typed return values for each verb. All implement
// [cmdutil.Result] (JSON() any, Human() string) duck-typed so the
// duck package does not import cmdutil and stays leaf-level.
//
// Convention: Human() returns the duckdb -box rendering captured at the
// same moment the structured payload is read, so JSON and human output
// stay consistent.

// ColumnInfo is one row of DESCRIBE: a column name and its duckdb type.
type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ColsResult is the result of [Cols].
type ColsResult struct {
	Path     string       `json:"path"`
	Table    string       `json:"table,omitempty"`
	Columns  []ColumnInfo `json:"columns"`
	humanBox string
}

// JSON satisfies cmdutil.Result.
func (r *ColsResult) JSON() any { return r }

// Human satisfies cmdutil.Result.
func (r *ColsResult) Human() string { return r.humanBox }

// RowsResult is the result of [Head], [Tail], and [Query].
type RowsResult struct {
	Path     string           `json:"path"`
	Table    string           `json:"table,omitempty"`
	Columns  []string         `json:"columns"`
	Rows     []map[string]any `json:"rows"`
	humanBox string
}

func (r *RowsResult) JSON() any     { return r }
func (r *RowsResult) Human() string { return r.humanBox }

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
	Columns  []GlimpseColumn `json:"columns"`
	humanBox string
}

func (r *GlimpseResult) JSON() any     { return r }
func (r *GlimpseResult) Human() string { return r.humanBox }

// ShapeResult is the result of [Shape].
type ShapeResult struct {
	Path     string `json:"path"`
	Table    string `json:"table,omitempty"`
	Rows     int    `json:"rows"`
	Columns  int    `json:"columns"`
	humanBox string
}

func (r *ShapeResult) JSON() any     { return r }
func (r *ShapeResult) Human() string { return r.humanBox }

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
	Path     string            `json:"path"`
	Table    string            `json:"table,omitempty"`
	Columns  []SummarizeColumn `json:"columns"`
	humanBox string
}

func (r *SummarizeResult) JSON() any     { return r }
func (r *SummarizeResult) Human() string { return r.humanBox }

// ConvertResult is the result of [Convert].
type ConvertResult struct {
	Input     string `json:"input"`
	Output    string `json:"output"`
	Rows      int    `json:"rows"`
	humanText string
}

func (r *ConvertResult) JSON() any     { return r }
func (r *ConvertResult) Human() string { return r.humanText }
