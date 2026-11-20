package export

import (
	"fmt"
	"io"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

const (
	maxColumnWidth = 50
	minColumnWidth = 8
	moneyFormat    = `"S/ "#,##0.00`
	dateFormat     = "dd/mm/yyyy"
)

// sheetName keeps the Excel rules: at most 31 characters and none of : \ / ? * [ ].
func sheetName(name string) string {
	if name == "" {
		name = "Reporte"
	}
	out := make([]rune, 0, 31)
	for _, r := range name {
		switch r {
		case ':', '\\', '/', '?', '*', '[', ']':
			continue
		}
		if len(out) == 31 {
			break
		}
		out = append(out, r)
	}
	return string(out)
}

// WriteXLSX writes one sheet: the header row (bold, brand fill, frozen, with autofilter), then the rows with
// money as numbers (S/ with two decimals) and dates as real dates (dd/mm/yyyy), and the optional total row.
func WriteXLSX(w io.Writer, r Report) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := sheetName(r.Sheet)
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	_ = f.SetDocProps(&excelize.DocProperties{Title: r.Title, Creator: "Pulso", Created: r.GeneratedAt.UTC().Format(time.RFC3339)})

	brand := ParseColor(r.Brand.Color)
	styles, err := newStyles(f, brand)
	if err != nil {
		return err
	}

	widths := make([]int, len(r.Columns))
	for i, c := range r.Columns {
		ref, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellStr(sheet, ref, c.Header); err != nil {
			return err
		}
		widths[i] = utf8.RuneCountInString(c.Header) + 4 // room for the filter button
	}
	last, _ := excelize.CoordinatesToCellName(len(r.Columns), 1)
	if err := f.SetCellStyle(sheet, "A1", last, styles.header); err != nil {
		return err
	}
	_ = f.SetRowHeight(sheet, 1, 22)

	for rowIndex, row := range r.Rows {
		if err := writeRow(f, sheet, rowIndex+2, r.Columns, row, styles, false, widths); err != nil {
			return err
		}
	}
	lastRow := len(r.Rows) + 1
	if r.TotalRow != nil {
		if err := writeRow(f, sheet, lastRow+1, r.Columns, r.TotalRow, styles, true, widths); err != nil {
			return err
		}
	}

	for i, c := range r.Columns {
		width := widths[i]
		if c.Width > 0 && int(c.Width) > width {
			width = int(c.Width)
		}
		width = min(max(width, minColumnWidth), maxColumnWidth)
		name, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, name, name, float64(width)+1); err != nil {
			return err
		}
	}
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	filterEnd, _ := excelize.CoordinatesToCellName(len(r.Columns), max(lastRow, 1))
	if err := f.AutoFilter(sheet, "A1:"+filterEnd, nil); err != nil {
		return err
	}
	return f.Write(w)
}

type xlsxStyles struct {
	header                                    int
	text, money, date, integer, decimal       int
	totalText, totalMoney, totalInt, totalDec int
}

func newStyles(f *excelize.File, brand RGB) (xlsxStyles, error) {
	var s xlsxStyles
	on := brand.OnColor()
	thin := []excelize.Border{{Type: "bottom", Color: "D9DEE3", Style: 1}}
	top := []excelize.Border{{Type: "top", Color: "263745", Style: 2}}
	defs := []struct {
		into  *int
		style excelize.Style
	}{
		{&s.header, excelize.Style{
			Font:      &excelize.Font{Bold: true, Color: on.Hex(), Size: 11},
			Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{brand.Hex()}},
			Alignment: &excelize.Alignment{Vertical: "center"},
		}},
		{&s.text, excelize.Style{Border: thin}},
		{&s.money, excelize.Style{Border: thin, CustomNumFmt: ptr(moneyFormat)}},
		{&s.date, excelize.Style{Border: thin, CustomNumFmt: ptr(dateFormat), Alignment: &excelize.Alignment{Horizontal: "left"}}},
		{&s.integer, excelize.Style{Border: thin, NumFmt: 3}},
		{&s.decimal, excelize.Style{Border: thin, CustomNumFmt: ptr("0.0")}},
		{&s.totalText, excelize.Style{Border: top, Font: &excelize.Font{Bold: true}}},
		{&s.totalMoney, excelize.Style{Border: top, Font: &excelize.Font{Bold: true}, CustomNumFmt: ptr(moneyFormat)}},
		{&s.totalInt, excelize.Style{Border: top, Font: &excelize.Font{Bold: true}, NumFmt: 3}},
		{&s.totalDec, excelize.Style{Border: top, Font: &excelize.Font{Bold: true}, CustomNumFmt: ptr("0.0")}},
	}
	for _, d := range defs {
		id, err := f.NewStyle(&d.style)
		if err != nil {
			return s, err
		}
		*d.into = id
	}
	return s, nil
}

func ptr[T any](v T) *T { return &v }

func writeRow(f *excelize.File, sheet string, rowNumber int, columns []Column, row []string, s xlsxStyles, total bool, widths []int) error {
	for i, c := range columns {
		ref, _ := excelize.CoordinatesToCellName(i+1, rowNumber)
		raw := cell(row, i)
		style := s.text
		if total {
			style = s.totalText
		}
		var value any = raw
		shown := raw
		switch c.Kind {
		case Money, Integer, Decimal:
			if v, err := strconv.ParseFloat(raw, 64); err == nil {
				value = v
				switch {
				case c.Kind == Money && total:
					style = s.totalMoney
				case c.Kind == Money:
					style = s.money
				case c.Kind == Integer && total:
					style = s.totalInt
				case c.Kind == Integer:
					style = s.integer
				case total:
					style = s.totalDec
				default:
					style = s.decimal
				}
				shown = display(c.Kind, raw) + "  "
			}
		case Date:
			if t, err := time.Parse("2006-01-02", raw); err == nil {
				value = t
				if !total {
					style = s.date
				}
				shown = "00/00/0000"
			}
		}
		if text, ok := value.(string); ok {
			if err := f.SetCellStr(sheet, ref, text); err != nil {
				return fmt.Errorf("cell %s: %w", ref, err)
			}
		} else if err := f.SetCellValue(sheet, ref, value); err != nil {
			return fmt.Errorf("cell %s: %w", ref, err)
		}
		if err := f.SetCellStyle(sheet, ref, ref, style); err != nil {
			return err
		}
		if n := utf8.RuneCountInString(shown); n > widths[i] {
			widths[i] = n
		}
	}
	return nil
}
