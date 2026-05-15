package duck

// xlsx_sheets.go — enumerate sheet names from an .xlsx/.xlsm file by parsing
// xl/workbook.xml inside the container. Pure stdlib (archive/zip + encoding/xml);
// duckdb v1.5.2 has no read_xlsx_metadata function so we do this ourselves.

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"

	"github.com/samber/lo"
)

// listSheets returns user-visible sheet names in workbook order.
func listSheets(path string) ([]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer func() { _ = zr.Close() }()

	var workbookEntry *zip.File
	for _, f := range zr.File {
		if f.Name == "xl/workbook.xml" {
			workbookEntry = f
			break
		}
	}
	if workbookEntry == nil {
		return nil, fmt.Errorf("not a valid xlsx: missing xl/workbook.xml")
	}

	rc, err := workbookEntry.Open()
	if err != nil {
		return nil, fmt.Errorf("open workbook.xml: %w", err)
	}
	defer func() { _ = rc.Close() }()

	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read workbook.xml: %w", err)
	}

	var wb struct {
		Sheets struct {
			Sheet []struct {
				Name string `xml:"name,attr"`
			} `xml:"sheet"`
		} `xml:"sheets"`
	}
	if err := xml.Unmarshal(body, &wb); err != nil {
		return nil, fmt.Errorf("parse workbook.xml: %w", err)
	}

	return lo.Map(wb.Sheets.Sheet, func(s struct {
		Name string `xml:"name,attr"`
	}, _ int) string {
		return s.Name
	}), nil
}
