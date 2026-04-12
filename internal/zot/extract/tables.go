package extract

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samber/lo"
)

// doclingTable is the minimum shape we read from DoclingDocument's
// tables[i]. The real schema is much richer (cell bounding boxes, row
// spans, column headers, etc.); for CSV post-processing we only need
// the pre-expanded grid docling gives us via data.grid[row][col].text.
type doclingTable struct {
	Data struct {
		NumRows int            `json:"num_rows"`
		NumCols int            `json:"num_cols"`
		Grid    [][]doclingCel `json:"grid"`
	} `json:"data"`
}

type doclingCel struct {
	Text         string `json:"text"`
	ColumnHeader bool   `json:"column_header"`
}

type doclingDoc struct {
	Tables []doclingTable `json:"tables"`
}

// writeTablesAsCSV parses the DoclingDocument JSON at jsonPath and
// writes one CSV per table to csvDir. Returns the absolute paths of the
// files written, in table-001, table-002, ... order. csvDir is created
// on demand and left uncreated if the document has no tables.
//
// Cell text is written verbatim through encoding/csv so commas and
// quotes get escaped. Column-header rows are just the first rows of the
// grid — no special treatment, because downstream consumers can decide
// whether to treat the first row as a header.
func writeTablesAsCSV(jsonPath, csvDir string) ([]string, error) {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", jsonPath, err)
	}
	var doc doclingDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", jsonPath, err)
	}
	if len(doc.Tables) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(csvDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", csvDir, err)
	}
	paths := make([]string, 0, len(doc.Tables))
	for i, t := range doc.Tables {
		name := fmt.Sprintf("table-%03d.csv", i+1)
		p := filepath.Join(csvDir, name)
		if err := writeOneTable(p, t); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func writeOneTable(path string, t doclingTable) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	w := csv.NewWriter(f)
	for _, row := range t.Data.Grid {
		rec := lo.Map(row, func(cell doclingCel, _ int) string {
			return cell.Text
		})
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
