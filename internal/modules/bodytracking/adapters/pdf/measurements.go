// Package pdf draws the measurements report of a member: letterhead, who, a summary of the latest values against
// the first ones, the weight over time and every measurement with its notes.
package pdf

import (
	"encoding/json"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/export"
)

// Title of the document.
const Title = "Seguimiento corporal"

var documentLabels = map[string]string{"dni": "DNI", "ce": "CE"}

// metric is one measured value of a measurement.
type metric struct {
	label string
	unit  string
	value func(app.MeasurementView) *json.Number
}

var metrics = []metric{
	{"Peso", "kg", func(m app.MeasurementView) *json.Number { return m.WeightKg }},
	{"Talla", "cm", func(m app.MeasurementView) *json.Number { return m.HeightCm }},
	{"% grasa", "%", func(m app.MeasurementView) *json.Number { return m.BodyFatPct }},
	{"Cintura", "cm", func(m app.MeasurementView) *json.Number { return m.WaistCm }},
	{"Pecho", "cm", func(m app.MeasurementView) *json.Number { return m.ChestCm }},
	{"Cadera", "cm", func(m app.MeasurementView) *json.Number { return m.HipCm }},
}

func number(n *json.Number) (float64, bool) {
	if n == nil {
		return 0, false
	}
	v, err := n.Float64()
	return v, err == nil
}

func withUnit(v float64, unit string) string {
	text := strconv.FormatFloat(v, 'f', 1, 64)
	if unit == "%" {
		return text + " %"
	}
	return text + " " + unit
}

// signed shows a change with its sign: "+1.2 kg", "−0.8 kg" or "Sin cambio".
func signed(delta float64, unit string) string {
	delta = math.Round(delta*10) / 10
	switch {
	case delta > 0:
		return "+" + withUnit(delta, unit)
	case delta < 0:
		return "−" + withUnit(-delta, unit)
	}
	return "Sin cambio"
}

func dateOf(raw string) string { return export.FormatDate(raw) }

// Render writes the PDF. The measurements come newest first (as the API lists them).
func Render(w io.Writer, brand export.Brand, generatedAt time.Time, report app.MeasurementReport) error {
	d := export.NewDoc(brand, Title, generatedAt, false)
	list := report.Measurements
	chronological := make([]app.MeasurementView, len(list))
	for i, m := range list {
		chronological[len(list)-1-i] = m
	}

	memberBlock(d, report.Member, chronological)
	d.SectionTitle("Resumen")
	if len(list) == 0 {
		d.TextColor(export.Muted)
		d.Font(export.Regular, 9.5)
		d.SetX(d.Left)
		d.CellFormat(d.Width, 10, "Todavía no hay mediciones registradas.", "", 1, "LM", false, 0, "")
		return d.Finish(w)
	}
	summary(d, chronological)
	d.SectionTitle("Peso en el tiempo (kg)")
	weightChart(d, chronological)
	d.SectionTitle("Mediciones")
	table(d, list)
	notes(d, list)
	return d.Finish(w)
}

func memberBlock(d *export.Doc, member app.MemberCard, chronological []app.MeasurementView) {
	y := d.GetY()
	d.TextColor(export.Ink)
	d.Font(export.SemiBold, 16)
	d.SetXY(d.Left, y)
	d.CellFormat(d.Width*0.62, 8, d.Fit(member.FullName, d.Width*0.62), "", 0, "LM", false, 0, "")
	document := documentLabels[member.DocumentType]
	if document == "" {
		document = strings.ToUpper(member.DocumentType)
	}
	d.TextColor(export.Muted)
	d.Font(export.Regular, 9.5)
	d.SetXY(d.Left, y+8.5)
	d.CellFormat(d.Width*0.62, 5, strings.TrimSpace(document+" "+member.DocumentNumber), "", 0, "LM", false, 0, "")

	right := d.Width * 0.38
	count := strconv.Itoa(len(chronological)) + " mediciones"
	if len(chronological) == 1 {
		count = "1 medición"
	}
	d.Font(export.SemiBold, 9.5)
	d.TextColor(export.Ink)
	d.SetXY(d.Right-right, y+1)
	d.CellFormat(right, 6, count, "", 0, "RM", false, 0, "")
	if len(chronological) > 0 {
		first, last := chronological[0].MeasuredOn, chronological[len(chronological)-1].MeasuredOn
		span := dateOf(first)
		if first != last {
			span = "Del " + dateOf(first) + " al " + dateOf(last)
		}
		d.Font(export.Regular, 9)
		d.TextColor(export.Muted)
		d.SetXY(d.Right-right, y+8.5)
		d.CellFormat(right, 5, span, "", 0, "RM", false, 0, "")
	}
	d.SetXY(d.Left, y+19)
}

// summary draws one card per metric: the latest value and its change since the first measurement.
func summary(d *export.Doc, chronological []app.MeasurementView) {
	const columns, h, gap = 3, 23.0, 4.0
	w := (d.Width - gap*(columns-1)) / columns
	top := d.GetY()
	tint := d.Color.Mix(0.93)
	for i, m := range metrics {
		x := d.Left + float64(i%columns)*(w+gap)
		y := top + float64(i/columns)*(h+gap)
		var first, last *app.MeasurementView
		for j := range chronological {
			if _, ok := number(m.value(chronological[j])); ok {
				if first == nil {
					first = &chronological[j]
				}
				last = &chronological[j]
			}
		}
		d.Fill(tint)
		d.RoundedRect(x, y, w, h, 2.5, "1234", "F")
		d.Fill(d.Color)
		d.Rect(x, y+3.5, 1.2, h-7, "F")
		d.TextColor(export.Muted)
		d.Font(export.Regular, 8.5)
		d.SetXY(x+5, y+2.5)
		d.CellFormat(w-8, 5, m.label, "", 0, "LM", false, 0, "")
		d.SetXY(x+5, y+8)
		if last == nil {
			d.TextColor(export.Muted)
			d.Font(export.Bold, 14)
			d.CellFormat(w-8, 7, "—", "", 0, "LM", false, 0, "")
			d.Font(export.Regular, 8)
			d.SetXY(x+5, y+15.5)
			d.CellFormat(w-8, 5, "Sin datos", "", 0, "LM", false, 0, "")
			continue
		}
		latest, _ := number(m.value(*last))
		d.TextColor(export.Ink)
		d.Font(export.Bold, 14)
		d.CellFormat(w-8, 7, withUnit(latest, m.unit), "", 0, "LM", false, 0, "")
		d.SetXY(x+5, y+15.5)
		if first == last {
			d.TextColor(export.Muted)
			d.Font(export.Regular, 8)
			d.CellFormat(w-8, 5, "Una sola medición", "", 0, "LM", false, 0, "")
			continue
		}
		initial, _ := number(m.value(*first))
		change := signed(latest-initial, m.unit)
		d.TextColor(d.Color)
		d.Font(export.SemiBold, 8.5)
		changeW := d.GetStringWidth(change) + 1.5
		d.CellFormat(changeW, 5, change, "", 0, "LM", false, 0, "")
		d.TextColor(export.Muted)
		d.Font(export.Regular, 8)
		d.CellFormat(w-8-changeW, 5, d.Fit("desde "+dateOf(first.MeasuredOn), w-8-changeW), "", 0, "LM", false, 0, "")
	}
	rows := (len(metrics) + columns - 1) / columns
	d.SetXY(d.Left, top+float64(rows)*(h+gap)+3)
}

type point struct {
	day   time.Time
	value float64
}

// niceStep is 1, 2 or 5 times a power of ten, giving about `ticks` divisions of span.
func niceStep(span float64, ticks int) float64 {
	raw := span / float64(ticks)
	magnitude := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, f := range []float64{1, 2, 2.5, 5, 10} {
		if raw <= f*magnitude {
			return f * magnitude
		}
	}
	return 10 * magnitude
}

// weightChart draws the weight over time: gridlines with values, the line with its points and the area below.
func weightChart(d *export.Doc, chronological []app.MeasurementView) {
	var points []point
	for _, m := range chronological {
		if v, ok := number(m.WeightKg); ok {
			day, err := time.Parse("2006-01-02", m.MeasuredOn)
			if err == nil {
				points = append(points, point{day, v})
			}
		}
	}
	if len(points) < 2 {
		d.TextColor(export.Muted)
		d.Font(export.Regular, 9.5)
		d.SetX(d.Left)
		d.CellFormat(d.Width, 10, "Se necesitan al menos dos pesos para dibujar la evolución.", "", 1, "LM", false, 0, "")
		d.Ln(2)
		return
	}
	const height, axisW, labelH = 62.0, 13.0, 7.0
	d.Ensure(height + labelH + 4)
	top := d.GetY() + 2
	left, right := d.Left+axisW, d.Right-4
	bottom := top + height

	lo, hi := points[0].value, points[0].value
	for _, p := range points {
		lo, hi = math.Min(lo, p.value), math.Max(hi, p.value)
	}
	span := math.Max(hi-lo, 2)
	step := niceStep(span*1.3, 4)
	minY := math.Floor((lo-span*0.15)/step) * step
	maxY := math.Ceil((hi+span*0.15)/step) * step
	yOf := func(v float64) float64 { return bottom - (v-minY)/(maxY-minY)*height }

	first, last := points[0].day, points[len(points)-1].day
	byIndex := !last.After(first)
	xOf := func(i int) float64 {
		if byIndex {
			return left + float64(i)/float64(len(points)-1)*(right-left)
		}
		return left + points[i].day.Sub(first).Hours()/last.Sub(first).Hours()*(right-left)
	}

	// Gridlines and their values.
	d.SetLineWidth(0.2)
	d.Font(export.Regular, 7.5)
	for v := minY; v <= maxY+step/2; v += step {
		y := yOf(v)
		d.Stroke(export.Line)
		d.Line(left, y, right, y)
		d.TextColor(export.Muted)
		d.SetXY(d.Left, y-2.5)
		d.CellFormat(axisW-2.5, 5, strconv.FormatFloat(v, 'f', decimals(step), 64), "", 0, "RM", false, 0, "")
	}

	// Area under the line, then the line, then the points.
	area := []fpdf.PointType{{X: xOf(0), Y: bottom}}
	for i, p := range points {
		area = append(area, fpdf.PointType{X: xOf(i), Y: yOf(p.value)})
	}
	area = append(area, fpdf.PointType{X: xOf(len(points) - 1), Y: bottom})
	d.Fill(d.Color.Mix(0.85))
	d.SetAlpha(0.6, "Normal")
	d.Polygon(area, "F")
	d.SetAlpha(1, "Normal")

	d.Stroke(d.Color)
	d.SetLineWidth(0.8)
	d.SetLineCapStyle("round")
	d.SetLineJoinStyle("round")
	for i := 1; i < len(points); i++ {
		d.Line(xOf(i-1), yOf(points[i-1].value), xOf(i), yOf(points[i].value))
	}
	radius := 1.1
	if len(points) > 40 {
		radius = 0.6
	}
	d.SetLineWidth(0.5)
	d.Fill(export.Canvas)
	for i, p := range points {
		d.Circle(xOf(i), yOf(p.value), radius, "FD")
	}

	// Values of the first and last points.
	d.Font(export.SemiBold, 8)
	d.TextColor(export.Ink)
	for _, i := range []int{0, len(points) - 1} {
		label := withUnit(points[i].value, "kg")
		lw := d.GetStringWidth(label) + 2
		x := min(max(xOf(i)-lw/2, left), right-lw)
		d.SetXY(x, yOf(points[i].value)-7)
		d.CellFormat(lw, 4.5, label, "", 0, "CM", false, 0, "")
	}

	// Baseline and dates: the first, the last and a few evenly spread between them.
	d.Stroke(export.Muted)
	d.SetLineWidth(0.3)
	d.Line(left, bottom, right, bottom)
	d.Font(export.Regular, 7.5)
	d.TextColor(export.Muted)
	labels := min(len(points), 6)
	used := map[int]bool{}
	lastRight := -1.0
	for k := 0; k < labels; k++ {
		i := int(math.Round(float64(k) * float64(len(points)-1) / float64(labels-1)))
		if used[i] {
			continue
		}
		used[i] = true
		text := points[i].day.Format("02/01/06")
		tw := d.GetStringWidth(text) + 2
		x := min(max(xOf(i)-tw/2, left-tw/2), right-tw/2)
		if x < lastRight+1 {
			continue
		}
		lastRight = x + tw
		d.SetXY(x, bottom+1.5)
		d.CellFormat(tw, 4.5, text, "", 0, "CM", false, 0, "")
	}
	d.SetXY(d.Left, bottom+labelH+4)
}

func decimals(step float64) int {
	if step != math.Trunc(step) {
		return 1
	}
	return 0
}

func table(d *export.Doc, newestFirst []app.MeasurementView) {
	columns := []export.TableColumn{
		{Header: "Fecha", Width: 14},
		{Header: "Peso (kg)", Width: 12, Right: true},
		{Header: "Talla (cm)", Width: 12, Right: true},
		{Header: "% grasa", Width: 11, Right: true},
		{Header: "Cintura (cm)", Width: 13, Right: true},
		{Header: "Pecho (cm)", Width: 12, Right: true},
		{Header: "Cadera (cm)", Width: 12, Right: true},
	}
	rows := tableRows(newestFirst)
	d.Table(columns, rows, nil)
	d.Ln(6)
}

// notes lists the notes of the measurements that have them, newest first.
func notes(d *export.Doc, newestFirst []app.MeasurementView) {
	var withNotes []app.MeasurementView
	for _, m := range newestFirst {
		if strings.TrimSpace(m.Notes) != "" {
			withNotes = append(withNotes, m)
		}
	}
	if len(withNotes) == 0 {
		return
	}
	d.SectionTitle("Notas")
	const dateW, lineH = 26.0, 4.8
	textW := d.Width - dateW
	for i, m := range withNotes {
		d.Font(export.Regular, 9)
		lines := d.Wrap(m.Notes, textW)
		height := float64(len(lines))*lineH + 4
		d.Ensure(min(height, 30))
		y := d.GetY()
		if i > 0 {
			d.Stroke(export.Line)
			d.SetLineWidth(0.2)
			d.Line(d.Left, y-2, d.Right, y-2)
		}
		d.TextColor(export.Ink)
		d.Font(export.SemiBold, 9)
		d.SetXY(d.Left, y)
		d.CellFormat(dateW, lineH, dateOf(m.MeasuredOn), "", 0, "LM", false, 0, "")
		d.Font(export.Regular, 9)
		for _, line := range lines {
			if d.Ensure(lineH) {
				y = d.GetY()
			}
			d.SetXY(d.Left+dateW, y)
			d.CellFormat(textW, lineH, line, "", 0, "LM", false, 0, "")
			y += lineH
			d.SetY(y)
		}
		d.SetY(y + 4)
	}
}

// tableRows is one row per measurement of the table: the date and every metric ("—" when absent).
func tableRows(newestFirst []app.MeasurementView) [][]string {
	rows := make([][]string, 0, len(newestFirst))
	for _, m := range newestFirst {
		row := []string{dateOf(m.MeasuredOn)}
		for _, metric := range metrics {
			if v, ok := number(metric.value(m)); ok {
				row = append(row, strconv.FormatFloat(v, 'f', 1, 64))
			} else {
				row = append(row, "—")
			}
		}
		rows = append(rows, row)
	}
	return rows
}
