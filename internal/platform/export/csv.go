package export

import (
	"encoding/csv"
	"io"
)

// WriteCSV writes UTF-8 with BOM (so Excel reads accents), comma separated, the header row first. Text cells
// that look like formulas are prefixed with a quote; numbers and dates are written as the API's text.
func WriteCSV(w io.Writer, r Report) error {
	if _, err := io.WriteString(w, "\xEF\xBB\xBF"); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	header := make([]string, len(r.Columns))
	for i, c := range r.Columns {
		header[i] = c.Header
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	record := make([]string, len(r.Columns))
	for _, row := range r.Rows {
		for i, c := range r.Columns {
			value := cell(row, i)
			if c.Kind == Text {
				value = SafeCell(value)
			}
			record[i] = value
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func cell(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}
