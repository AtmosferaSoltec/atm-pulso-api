package export

import (
	"bytes"
	"encoding/csv"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func sample(rows int) Report {
	r := Report{
		Title: "Reporte de pagos", Sheet: "Pagos",
		Brand:       Brand{Name: "Gimnasio Peña & Núñez", Color: "#17677b"},
		GeneratedAt: time.Date(2026, 11, 20, 20, 42, 0, 0, time.UTC),
		Filters:     []Filter{{"Período", "01/11/2026 – 20/11/2026"}, {"Medio de pago", "Todos"}, {"Sede", "Miraflores"}},
		Figures:     []Figure{{"Pagos", "3"}, {"Total cobrado", "S/ 1,245.80"}, {"Efectivo", "S/ 120.00"}, {"Yape/Plin", "S/ 1,125.80"}},
		Columns: []Column{
			{Header: "Boleta", Width: 10}, {Header: "Miembro", Width: 26}, {Header: "Documento", Width: 12},
			{Header: "Plan", Width: 18}, {Header: "Importe", Kind: Money, Width: 12}, {Header: "Medio de pago", Width: 13},
			{Header: "Fecha", Kind: Date, Width: 11},
		},
		TotalRow: []string{"Total", "", "", "", "1245.80", "", ""},
	}
	names := []string{"José Ñañez Peña", "María Fernández Íñiguez", "=HYPERLINK(\"x\")", "Ángela Muñoz"}
	for i := range rows {
		r.Rows = append(r.Rows, []string{"R-" + strings.Repeat("0", 5) + string(rune('1'+i%9)), names[i%len(names)], "4567890" + string(rune('0'+i%10)),
			"Mensual", "415.27", "Yape/Plin", "2026-11-" + string(rune('0'+(i%2+1))) + string(rune('0'+i%10))})
	}
	return r
}

func TestCSVAndExcelShareColumnsHeadersAndOrder(t *testing.T) {
	r := sample(3)
	var csvOut, xlsxOut bytes.Buffer
	require.NoError(t, WriteCSV(&csvOut, r))
	require.NoError(t, WriteXLSX(&xlsxOut, r))

	require.True(t, bytes.HasPrefix(csvOut.Bytes(), []byte("\xEF\xBB\xBF")))
	records, err := csv.NewReader(bytes.NewReader(csvOut.Bytes()[3:])).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 4, "header and three rows; the total row is not in the CSV")
	assert.Equal(t, "'=HYPERLINK(\"x\")", records[3][1], "formulas are neutralized")

	book, err := excelize.OpenReader(&xlsxOut)
	require.NoError(t, err)
	sheet := book.GetSheetName(0)
	assert.Equal(t, "Pagos", sheet)
	rows, err := book.GetRows(sheet)
	require.NoError(t, err)
	assert.Equal(t, records[0], rows[0])

	// Money is a number with the S/ format, dates are real dates.
	amount, err := book.GetCellValue(sheet, "E2", excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	assert.Equal(t, "415.27", amount)
	shown, err := book.GetCellValue(sheet, "G2")
	require.NoError(t, err)
	assert.Equal(t, "10/11/2026", shown)
	panes, err := book.GetPanes(sheet)
	require.NoError(t, err)
	assert.True(t, panes.Freeze)
	assert.Equal(t, 1, panes.YSplit)
}

func TestFormatting(t *testing.T) {
	assert.Equal(t, "S/ 1,245.80", FormatMoney("1245.8"))
	assert.Equal(t, "S/ 0.00", FormatMoney("0"))
	assert.Equal(t, "S/ -12.50", FormatMoney("-12.5"))
	assert.Equal(t, "S/ 1,000,000.00", FormatMoney("1000000"))
	assert.Equal(t, "20/11/2026", FormatDate("2026-11-20"))
	assert.Equal(t, "20/11/2026 15:42", FormatDateTime(time.Date(2026, 11, 20, 20, 42, 0, 0, time.UTC)))
	assert.Equal(t, "GP", Initials("gimnasio Peña"))
	assert.Equal(t, "ÑA", Initials("ñandú atlético club"))
	assert.Equal(t, "P", Initials(""))
	assert.Equal(t, RGB{255, 255, 255}, ParseColor("#17677b").OnColor())
	assert.Equal(t, RGB{0x14, 0x26, 0x30}, ParseColor("#fde047").OnColor())
	assert.Equal(t, ParseColor(DefaultColor), ParseColor("nope"))
	f, ok := ParseFormat("")
	assert.True(t, ok)
	assert.Equal(t, CSV, f)
	_, ok = ParseFormat("docx")
	assert.False(t, ok)
	assert.Equal(t, "pagos-2026-11-20.xlsx", Filename("pagos", time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), XLSX))
}

func TestPDFRendersManyPagesAndEmptyLists(t *testing.T) {
	for _, rows := range []int{0, 3, 90} {
		var out bytes.Buffer
		require.NoError(t, WritePDF(&out, sample(rows)))
		assert.True(t, bytes.HasPrefix(out.Bytes(), []byte("%PDF-")))
		if dir := os.Getenv("EXPORT_SAMPLE_DIR"); dir != "" {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "table-"+string(rune('a'+rows%26))+".pdf"), out.Bytes(), 0o600))
		}
	}
}

// testLogo is a wide two-colour PNG with transparency, like a real logo.
func testLogo(t *testing.T) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 360, 120))
	for x := 0; x < 360; x++ {
		for y := 0; y < 120; y++ {
			switch {
			case (x-60)*(x-60)+(y-60)*(y-60) < 50*50:
				img.Set(x, y, color.NRGBA{0xe1, 0x5a, 0x2b, 0xff})
			case x > 130 && y > 40 && y < 80:
				img.Set(x, y, color.NRGBA{0x26, 0x37, 0x45, 0xff})
			}
		}
	}
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, img))
	return out.Bytes()
}

func TestPDFWithLogo(t *testing.T) {
	r := sample(12)
	r.Brand.Logo = testLogo(t)
	r.Brand.Color = "#e15a2b"
	var out bytes.Buffer
	require.NoError(t, WritePDF(&out, r))
	assert.True(t, bytes.HasPrefix(out.Bytes(), []byte("%PDF-")))
	if dir := os.Getenv("EXPORT_SAMPLE_DIR"); dir != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "table-logo.pdf"), out.Bytes(), 0o600))
	}
}

func TestABadLogoNeverBreaksThePDF(t *testing.T) {
	r := sample(2)
	r.Brand.Logo = []byte("not a png")
	var out bytes.Buffer
	require.NoError(t, WritePDF(&out, r))
	assert.True(t, bytes.HasPrefix(out.Bytes(), []byte("%PDF-")))
}
