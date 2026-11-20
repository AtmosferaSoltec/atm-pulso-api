// Package export renders a tabular report as CSV, Excel (.xlsx) or a designed PDF, and gives the shared
// pieces of every Pulso PDF (fonts, letterhead, footer). It knows nothing of the modules: a module builds a
// Report (columns, rows as text, filters, totals) and picks the format; the same Report always yields the
// same columns, headers and order in every format.
//
// Values travel as the text the API already uses: money "1245.80", dates "2026-11-20", integers "3". The
// column Kind says how Excel and the PDF show them (a real number with 2 decimals, a real date dd/mm/yyyy);
// the CSV keeps the text as is.
package export

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// Format is a download format.
type Format string

// Supported formats.
const (
	CSV  Format = "csv"
	XLSX Format = "xlsx"
	PDF  Format = "pdf"
)

// ParseFormat reads the `format` query parameter; empty means CSV (the historical default).
func ParseFormat(raw string) (Format, bool) {
	switch Format(strings.ToLower(strings.TrimSpace(raw))) {
	case "", CSV:
		return CSV, true
	case XLSX:
		return XLSX, true
	case PDF:
		return PDF, true
	}
	return "", false
}

// ContentType is the media type of the format.
func (f Format) ContentType() string {
	switch f {
	case XLSX:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case PDF:
		return "application/pdf"
	default:
		return "text/csv; charset=utf-8"
	}
}

// Filename is "<base>-YYYY-MM-DD.<ext>", e.g. pagos-2026-11-20.xlsx. base must already be a safe ASCII slug.
func Filename(base string, day time.Time, f Format) string {
	return fmt.Sprintf("%s-%s.%s", base, day.Format("2006-01-02"), f)
}

// Disposition is the Content-Disposition header of a download (the file name is ASCII by construction).
func Disposition(filename string) string {
	return fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(filename, `"`, ""))
}

// Kind says how a column's values are shown.
type Kind int

// Column kinds.
const (
	// Text is shown as is (and guarded against spreadsheet formulas).
	Text Kind = iota
	// Money is "1245.80": a number with two decimals (S/) in Excel, right-aligned "S/ 1,245.80" in the PDF.
	Money
	// Date is "2026-11-20": a real date dd/mm/yyyy in Excel and the PDF.
	Date
	// Integer is "3": a whole number, right-aligned.
	Integer
	// Decimal is "72.5": a number with one decimal, right-aligned.
	Decimal
)

// Column is one column of a report.
type Column struct {
	Header string
	Kind   Kind
	// Width is the relative width in the PDF and roughly the characters in Excel (0 = 12).
	Width float64
}

// Filter is one applied filter shown on top of the PDF ("Período", "01/11/2026 – 20/11/2026").
type Filter struct {
	Label string
	Value string
}

// Figure is a highlighted total shown as a card in the PDF ("Total cobrado", "S/ 1,245.80").
type Figure struct {
	Label string
	Value string
}

// Brand is the letterhead of the gym. Logo is PNG bytes (nil: a monogram is drawn instead).
type Brand struct {
	Name  string
	Color string
	Logo  []byte
}

// Report is everything a download needs.
type Report struct {
	// Title is the human title ("Reporte de pagos"); Sheet the Excel tab name ("Pagos").
	Title string
	Sheet string
	Brand Brand
	// GeneratedAt is when the file was made (shown in Lima time).
	GeneratedAt time.Time
	Filters     []Filter
	Figures     []Figure
	Columns     []Column
	Rows        [][]string
	// TotalRow, when set, is a last bold row aligned to the columns (e.g. the sum of an amount column). It is
	// written to the PDF and to Excel only; the CSV stays a plain list.
	TotalRow []string
	// Landscape forces an A4 landscape PDF; otherwise it is landscape with more than six columns.
	Landscape bool
}

// Write renders the report in the format.
func Write(w io.Writer, f Format, r Report) error {
	switch f {
	case XLSX:
		return WriteXLSX(w, r)
	case PDF:
		return WritePDF(w, r)
	default:
		return WriteCSV(w, r)
	}
}

// SafeCell stops spreadsheet formulas: a text starting with = + - @ tab or CR is prefixed with a quote.
func SafeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
