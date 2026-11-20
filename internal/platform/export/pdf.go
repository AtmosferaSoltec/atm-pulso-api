package export

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// Inter (SIL Open Font License 1.1, which allows embedding; see fonts/LICENSE.txt) renders Spanish accents
// and ñ. Only the glyphs used are embedded in each PDF.
var (
	//go:embed fonts/Inter-Regular.ttf
	interRegular []byte
	//go:embed fonts/Inter-SemiBold.ttf
	interSemiBold []byte
	//go:embed fonts/Inter-Bold.ttf
	interBold []byte
)

// Font weights of the embedded family.
const (
	Regular  = "inter"
	SemiBold = "inter-semibold"
	Bold     = "inter-bold"
)

// Palette shared by every PDF.
var (
	Ink    = RGB{0x1f, 0x2a, 0x37}
	Muted  = RGB{0x6b, 0x77, 0x85}
	Line   = RGB{0xdf, 0xe3, 0xe8}
	Zebra  = RGB{0xf5, 0xf7, 0xf9}
	Canvas = RGB{0xff, 0xff, 0xff}
)

const (
	margin       = 14.0
	letterheadH  = 16.0
	contentTop   = margin + letterheadH + 6
	footerHeight = 12.0
	pageAlias    = "{nb}"
	logoBoxW     = 42.0
)

// Doc is an A4 page set with the gym letterhead on every page and "Página X de Y" in the footer. Modules draw
// their content with the embedded *fpdf.Fpdf between NewDoc and Finish.
type Doc struct {
	*fpdf.Fpdf
	Brand       Brand
	Color, On   RGB
	Title       string
	GeneratedAt time.Time
	// Left, Right and Width are the horizontal content limits; Bottom is the lowest y content may reach.
	Left, Right, Width, Bottom float64
	logo                       string
	logoW, logoH               float64
}

// NewDoc starts a document with its first page.
func NewDoc(brand Brand, title string, generatedAt time.Time, landscape bool) *Doc {
	orientation := "P"
	if landscape {
		orientation = "L"
	}
	pdf := fpdf.New(orientation, "mm", "A4", "")
	// The alias must be set before the fonts are added so its characters are part of the subset.
	pdf.AliasNbPages(pageAlias)
	pdf.AddUTF8FontFromBytes(Regular, "", interRegular)
	pdf.AddUTF8FontFromBytes(SemiBold, "", interSemiBold)
	pdf.AddUTF8FontFromBytes(Bold, "", interBold)
	pdf.SetMargins(margin, contentTop, margin)
	pdf.SetAutoPageBreak(false, margin)
	// Padding is managed by the drawing code, so Fit measures exactly what is shown.
	pdf.SetCellMargin(0)
	pdf.SetTitle(title, true)
	pdf.SetAuthor(brand.Name, true)
	pdf.SetCreator("Pulso", true)
	pdf.SetCreationDate(generatedAt)
	pdf.SetModificationDate(generatedAt)

	pageW, pageH := pdf.GetPageSize()
	d := &Doc{
		Fpdf: pdf, Brand: brand, Title: title, GeneratedAt: generatedAt,
		Color: ParseColor(brand.Color), Left: margin, Right: pageW - margin, Width: pageW - 2*margin,
		Bottom: pageH - margin - footerHeight,
	}
	d.On = d.Color.OnColor()
	d.registerLogo()
	pdf.SetHeaderFuncMode(d.letterhead, false)
	pdf.SetFooterFunc(d.footer)
	pdf.AddPage()
	return d
}

// registerLogo adds the logo when it is a PNG fpdf can read. It is first tried on a scratch document so a bad
// image never poisons the real one: a PDF must not fail because of the logo.
func (d *Doc) registerLogo() {
	if len(d.Brand.Logo) == 0 {
		return
	}
	options := fpdf.ImageOptions{ImageType: "PNG"}
	probe := fpdf.New("P", "mm", "A4", "")
	if info := probe.RegisterImageOptionsReader("logo", options, bytes.NewReader(d.Brand.Logo)); info == nil || probe.Err() {
		return
	}
	info := d.RegisterImageOptionsReader("logo", options, bytes.NewReader(d.Brand.Logo))
	if info == nil || d.Err() {
		return
	}
	w, h := info.Width(), info.Height()
	if w <= 0 || h <= 0 {
		return
	}
	// Fit inside logoBoxW x letterheadH keeping the aspect ratio.
	scale := min(logoBoxW/w, letterheadH/h)
	d.logo, d.logoW, d.logoH = "logo", w*scale, h*scale
}

// SetColor helpers.
func (d *Doc) Fill(c RGB)      { d.SetFillColor(c.R, c.G, c.B) }
func (d *Doc) Stroke(c RGB)    { d.SetDrawColor(c.R, c.G, c.B) }
func (d *Doc) TextColor(c RGB) { d.SetTextColor(c.R, c.G, c.B) }
func (d *Doc) Font(w string, size float64) {
	d.SetFont(w, "", size)
}

// letterhead draws the brand bar, the logo (or a monogram), the gym name, the title and the generation time.
func (d *Doc) letterhead() {
	pageW, _ := d.GetPageSize()
	d.Fill(d.Color)
	d.Rect(0, 0, pageW, 3, "F")

	top := margin - 2
	x := d.Left
	if d.logo != "" {
		d.ImageOptions(d.logo, x, top+(letterheadH-d.logoH)/2, d.logoW, d.logoH, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		x += d.logoW + 4
	} else {
		d.Fill(d.Color)
		d.RoundedRect(x, top, letterheadH, letterheadH, 3, "1234", "F")
		d.TextColor(d.On)
		d.Font(Bold, 15)
		d.SetXY(x, top)
		d.CellFormat(letterheadH, letterheadH, Initials(d.Brand.Name), "", 0, "CM", false, 0, "")
		x += letterheadH + 4
	}
	nameW := d.Right - x - 90
	d.TextColor(Ink)
	d.Font(SemiBold, 13)
	d.SetXY(x, top+2)
	d.CellFormat(nameW, 7, d.Fit(d.Brand.Name, nameW), "", 0, "LM", false, 0, "")
	d.TextColor(Muted)
	d.Font(Regular, 8.5)
	d.SetXY(x, top+9)
	d.CellFormat(nameW, 5, "Generado el "+FormatDateTime(d.GeneratedAt), "", 0, "LM", false, 0, "")

	d.TextColor(Ink)
	d.Font(Bold, 15)
	d.SetXY(d.Right-90, top+1)
	d.CellFormat(90, 8, d.Fit(d.Title, 90), "", 0, "RM", false, 0, "")
	d.Fill(d.Color)
	d.Rect(d.Right-18, top+10.5, 18, 1.2, "F")

	d.Stroke(Line)
	d.SetLineWidth(0.3)
	d.Line(d.Left, margin+letterheadH+1, d.Right, margin+letterheadH+1)
	d.SetXY(d.Left, contentTop)
}

func (d *Doc) footer() {
	_, pageH := d.GetPageSize()
	y := pageH - margin - 5
	d.Stroke(Line)
	d.SetLineWidth(0.3)
	d.Line(d.Left, y-2, d.Right, y-2)
	d.TextColor(Muted)
	d.Font(Regular, 8)
	d.SetXY(d.Left, y)
	d.CellFormat(d.Width/2, 5, d.Fit("Pulso · "+d.Brand.Name, d.Width/2), "", 0, "LM", false, 0, "")
	// The total is filled in at the end; the label is placed so "Página 12 de 12" ends at the margin.
	label := fmt.Sprintf("Página %d de %s", d.PageNo(), pageAlias)
	room := d.GetStringWidth(fmt.Sprintf("Página %d de %d", d.PageNo(), d.PageNo()))
	d.SetXY(d.Right-room, y)
	d.CellFormat(room, 5, label, "", 0, "LM", false, 0, "")
}

// Fit shortens text with an ellipsis so it fits in width with the current font.
func (d *Doc) Fit(text string, width float64) string {
	text = Clean(text)
	const tolerance = 0.01
	if d.GetStringWidth(text) <= width+tolerance {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := strings.TrimRight(string(runes), " ") + "…"
		if d.GetStringWidth(candidate) <= width+tolerance {
			return candidate
		}
	}
	return ""
}

// Ensure starts a new page when h millimetres do not fit; it reports whether it did.
func (d *Doc) Ensure(h float64) bool {
	if d.GetY()+h <= d.Bottom {
		return false
	}
	d.AddPage()
	return true
}

// SectionTitle draws a heading with a short brand accent underneath.
func (d *Doc) SectionTitle(text string) {
	d.Ensure(16)
	y := d.GetY()
	d.TextColor(Ink)
	d.Font(SemiBold, 11.5)
	d.SetXY(d.Left, y)
	d.CellFormat(d.Width, 7, text, "", 0, "LM", false, 0, "")
	d.Fill(d.Color)
	d.Rect(d.Left, y+7.2, 10, 0.9, "F")
	d.SetXY(d.Left, y+11)
}

// Filters draws the applied filters as chips that wrap.
func (d *Doc) Filters(filters []Filter) {
	if len(filters) == 0 {
		return
	}
	const h, gap, pad = 7.0, 2.5, 3.0
	x, y := d.Left, d.GetY()
	for _, f := range filters {
		d.Font(Regular, 8.5)
		labelW := d.GetStringWidth(f.Label + ": ")
		d.Font(SemiBold, 8.5)
		valueW := d.GetStringWidth(f.Value)
		w := min(labelW+valueW+2*pad, d.Width)
		if x+w > d.Right && x > d.Left {
			x, y = d.Left, y+h+gap
		}
		d.Fill(Zebra)
		d.Stroke(Line)
		d.SetLineWidth(0.25)
		d.RoundedRect(x, y, w, h, 2, "1234", "FD")
		d.SetXY(x+pad, y)
		d.Font(Regular, 8.5)
		d.TextColor(Muted)
		d.CellFormat(labelW, h, f.Label+": ", "", 0, "LM", false, 0, "")
		d.Font(SemiBold, 8.5)
		d.TextColor(Ink)
		d.CellFormat(w-labelW-2*pad, h, d.Fit(f.Value, w-labelW-2*pad), "", 0, "LM", false, 0, "")
		x += w + gap
	}
	d.SetXY(d.Left, y+h+5)
}

// Figures draws the highlighted totals as cards in one row.
func (d *Doc) Figures(figures []Figure) {
	if len(figures) == 0 {
		return
	}
	const h, gap = 17.0, 4.0
	d.Ensure(h + 4)
	y := d.GetY()
	w := (d.Width - gap*float64(len(figures)-1)) / float64(len(figures))
	tint := d.Color.Mix(0.92)
	for i, f := range figures {
		x := d.Left + float64(i)*(w+gap)
		d.Fill(tint)
		d.RoundedRect(x, y, w, h, 2.5, "1234", "F")
		d.Fill(d.Color)
		d.Rect(x, y+3, 1.2, h-6, "F")
		d.TextColor(Muted)
		d.Font(Regular, 8)
		d.SetXY(x+5, y+2.5)
		d.CellFormat(w-7, 5, d.Fit(f.Label, w-7), "", 0, "LM", false, 0, "")
		d.TextColor(Ink)
		d.Font(Bold, 13)
		d.SetXY(x+5, y+8)
		d.CellFormat(w-7, 7, d.Fit(f.Value, w-7), "", 0, "LM", false, 0, "")
	}
	d.SetXY(d.Left, y+h+6)
}

// TableColumn is a column of Doc.Table: a header, a relative width and whether values are right-aligned.
type TableColumn struct {
	Header string
	Width  float64
	Right  bool
}

// Table draws a table with a brand header (repeated on every page), zebra rows and an optional bold total
// row. Values are already formatted. Long values are cut with an ellipsis.
func (d *Doc) Table(columns []TableColumn, rows [][]string, total []string) {
	const headH, rowH, pad, size = 8.0, 6.8, 2.2, 8.3
	widths := d.columnWidths(columns, rows, total, size, pad)
	header := func() {
		y := d.GetY()
		d.Fill(d.Color)
		d.RoundedRect(d.Left, y, d.Width, headH, 1.5, "12", "F")
		d.TextColor(d.On)
		d.Font(SemiBold, size)
		x := d.Left
		for i, c := range columns {
			d.SetXY(x+pad, y)
			align := "LM"
			if c.Right {
				align = "RM"
			}
			d.CellFormat(widths[i]-2*pad, headH, d.Fit(c.Header, widths[i]-2*pad), "", 0, align, false, 0, "")
			x += widths[i]
		}
		d.SetXY(d.Left, y+headH)
	}
	line := func(values []string, fill *RGB, weight string) {
		y := d.GetY()
		if fill != nil {
			d.Fill(*fill)
			d.Rect(d.Left, y, d.Width, rowH, "F")
		}
		d.Font(weight, size)
		d.TextColor(Ink)
		x := d.Left
		for i, c := range columns {
			value := ""
			if i < len(values) {
				value = values[i]
			}
			align := "LM"
			if c.Right {
				align = "RM"
			}
			d.SetXY(x+pad, y)
			d.CellFormat(widths[i]-2*pad, rowH, d.Fit(value, widths[i]-2*pad), "", 0, align, false, 0, "")
			x += widths[i]
		}
		d.SetXY(d.Left, y+rowH)
	}

	if d.Ensure(headH + rowH*2) {
		d.SetY(contentTop)
	}
	header()
	if len(rows) == 0 {
		d.TextColor(Muted)
		d.Font(Regular, 9)
		d.SetX(d.Left)
		d.CellFormat(d.Width, 14, "No hay registros con estos filtros.", "", 1, "CM", false, 0, "")
		return
	}
	for i, values := range rows {
		if d.Ensure(rowH) {
			header()
		}
		var fill *RGB
		if i%2 == 1 {
			fill = &Zebra
		}
		line(values, fill, Regular)
	}
	d.Stroke(Line)
	d.SetLineWidth(0.3)
	d.Line(d.Left, d.GetY(), d.Right, d.GetY())
	if total != nil {
		if d.Ensure(rowH + 1) {
			header()
		}
		tint := d.Color.Mix(0.9)
		y := d.GetY()
		line(total, &tint, SemiBold)
		d.Stroke(d.Color)
		d.SetLineWidth(0.5)
		d.Line(d.Left, y, d.Right, y)
	}
}

// columnWidths sizes the columns to their content: each gets the width of its longest value or header; the room
// left over is shared by weight, and when the content does not fit, the text columns give up width (down to
// a floor) before anything else, so numbers and dates are never cut.
func (d *Doc) columnWidths(columns []TableColumn, rows [][]string, total []string, size, pad float64) []float64 {
	const textCap, textFloor = 75.0, 16.0
	natural := make([]float64, len(columns))
	d.Font(SemiBold, size)
	for i, c := range columns {
		natural[i] = d.GetStringWidth(Clean(c.Header))
	}
	measure := func(values []string, weight string) {
		d.Font(weight, size)
		for i := range columns {
			if i < len(values) && values[i] != "" {
				natural[i] = max(natural[i], d.GetStringWidth(Clean(values[i])))
			}
		}
	}
	for _, row := range rows {
		measure(row, Regular)
	}
	measure(total, SemiBold)
	var sum, weights float64
	for i, c := range columns {
		if !c.Right {
			natural[i] = min(natural[i], textCap)
		}
		natural[i] += 2*pad + 0.5
		sum += natural[i]
		weights += max(c.Width, 1)
	}
	widths := append([]float64(nil), natural...)
	if sum <= d.Width {
		extra := d.Width - sum
		for i, c := range columns {
			widths[i] += extra * max(c.Width, 1) / weights
		}
		return widths
	}
	// Too wide: shrink the text columns in proportion to what they have above the floor.
	over := sum - d.Width
	var slack float64
	for i, c := range columns {
		if !c.Right {
			slack += max(natural[i]-textFloor, 0)
		}
	}
	if slack > 0 {
		cut := min(over, slack)
		for i, c := range columns {
			if !c.Right {
				widths[i] -= cut * max(natural[i]-textFloor, 0) / slack
			}
		}
		over -= cut
	}
	if over > 0 {
		scale := d.Width / (d.Width + over)
		for i := range widths {
			widths[i] *= scale
		}
	}
	return widths
}

// Finish writes the document.
func (d *Doc) Finish(w io.Writer) error {
	if err := d.Error(); err != nil {
		return err
	}
	return d.Output(w)
}

// WritePDF renders a Report: letterhead, filters, totals cards and the table with its total row.
func WritePDF(w io.Writer, r Report) error {
	landscape := r.Landscape || len(r.Columns) > 6
	d := NewDoc(r.Brand, r.Title, r.GeneratedAt, landscape)
	d.Filters(r.Filters)
	d.Figures(r.Figures)
	columns := make([]TableColumn, len(r.Columns))
	for i, c := range r.Columns {
		width := c.Width
		if width == 0 {
			width = 12
		}
		columns[i] = TableColumn{Header: c.Header, Width: width, Right: rightAligned(c.Kind)}
	}
	rows := make([][]string, len(r.Rows))
	for i, row := range r.Rows {
		values := make([]string, len(r.Columns))
		for j, c := range r.Columns {
			values[j] = display(c.Kind, cell(row, j))
		}
		rows[i] = values
	}
	var total []string
	if r.TotalRow != nil {
		total = make([]string, len(r.Columns))
		for j, c := range r.Columns {
			total[j] = display(c.Kind, cell(r.TotalRow, j))
		}
	}
	d.Table(columns, rows, total)
	return d.Finish(w)
}

// Wrap splits text into lines that fit width with the current font, keeping the user's line breaks.
func (d *Doc) Wrap(text string, width float64) []string {
	var lines []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		paragraph = strings.TrimSpace(Clean(paragraph))
		if paragraph == "" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			continue
		}
		current := ""
		for _, word := range strings.Fields(paragraph) {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}
			if d.GetStringWidth(candidate) <= width {
				current = candidate
				continue
			}
			if current != "" {
				lines = append(lines, current)
			}
			// A single word longer than the line is cut.
			for d.GetStringWidth(word) > width {
				cut := d.Fit(word, width)
				cut = strings.TrimSuffix(cut, "…")
				if cut == "" {
					break
				}
				lines = append(lines, cut)
				word = strings.TrimPrefix(word, cut)
			}
			current = word
		}
		if current != "" {
			lines = append(lines, current)
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
