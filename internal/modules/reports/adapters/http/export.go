package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/reports/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/export"
)

var methodLabels = map[string]string{"yape_plin": "Yape/Plin", "cash": "Efectivo", "card": "Tarjeta", "transfer": "Transferencia"}

var kindLabels = map[string]string{"membership": "Membresías", "day_pass": "Pases del día"}

// paymentColumns are the same in the CSV, Excel and the PDF (the CSV keeps its historical headers).
var paymentColumns = []export.Column{
	{Header: "Boleta", Width: 11},
	{Header: "Miembro", Width: 28},
	{Header: "Documento", Width: 13},
	{Header: "Plan", Width: 18},
	{Header: "Importe", Kind: export.Money, Width: 13},
	{Header: "Medio de pago", Width: 14},
	{Header: "Fecha", Kind: export.Date, Width: 11},
}

// export answers GET /payments/export?format=csv|xlsx|pdf with the same filters as the list (admin only).
func (h *Handler) export(c *gin.Context) {
	format, ok := export.ParseFormat(c.Query("format"))
	if !ok {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "format", Code: "FORMAT_INVALID"}))
		return
	}
	q, ok := parseQuery(c)
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	data, err := h.service.ExportPayments(c.Request.Context(), p, q)
	if err != nil {
		respond(c, err)
		return
	}
	now := h.service.Clock.Now()
	report := export.Report{
		Title: "Reporte de pagos", Sheet: "Pagos", GeneratedAt: now,
		Columns: paymentColumns, Rows: paymentRows(data.Rows),
		Filters:  paymentFilters(data, q),
		Figures:  paymentFigures(data.Totals),
		TotalRow: []string{"Total", count(data.Totals.Count, "pago", "pagos"), "", "", data.Totals.Amount, "", ""},
	}
	if format == export.PDF {
		report.Brand = export.BrandOf(c.Request.Context(), h.brands, p.GymID, p.TenantID)
	}
	body, err := export.Render(format, report)
	if err != nil {
		apierror.Respond(c, err)
		return
	}
	c.Header("Content-Disposition", export.Disposition(export.Filename("pagos", clock.LimaDate(now), format)))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, format.ContentType(), body)
}

func paymentRows(rows []app.PaymentRow) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		name, document := "", ""
		if row.Member != nil {
			name = row.Member.FullName
		} else if row.VisitorName != nil {
			name = *row.VisitorName
		}
		if row.Document != nil {
			document = *row.Document
		}
		out = append(out, []string{row.Receipt.Number, name, document, row.PlanLabel, row.Amount, methodLabels[row.Method], row.PaidOn})
	}
	return out
}

// period is "20/11/2026" for one day or "01/11/2026 – 20/11/2026".
func period(from, to time.Time) string {
	if from.Equal(to) {
		return from.Format("02/01/2006")
	}
	return from.Format("02/01/2006") + " – " + to.Format("02/01/2006")
}

func paymentFilters(data app.PaymentExport, q app.PaymentQuery) []export.Filter {
	filters := []export.Filter{{Label: "Período", Value: period(data.From, data.To)}}
	branch := "Todas"
	if data.BranchName != "" {
		branch = data.BranchName
	}
	filters = append(filters, export.Filter{Label: "Sede", Value: branch})
	method := "Todos"
	if q.Method != "" {
		method = methodLabels[q.Method]
	}
	filters = append(filters, export.Filter{Label: "Medio de pago", Value: method})
	if q.Kind != "" {
		filters = append(filters, export.Filter{Label: "Tipo", Value: kindLabels[q.Kind]})
	}
	if q.Query != "" {
		filters = append(filters, export.Filter{Label: "Búsqueda", Value: q.Query})
	}
	return filters
}

func paymentFigures(t app.Totals) []export.Figure {
	average := "—"
	if t.Average != nil {
		average = export.FormatMoney(*t.Average)
	}
	return []export.Figure{
		{Label: "Pagos", Value: strconv.Itoa(t.Count)},
		{Label: "Total cobrado", Value: export.FormatMoney(t.Amount)},
		{Label: "Ticket promedio", Value: average},
		{Label: "Pases del día", Value: strconv.Itoa(t.DayPasses)},
	}
}

// count is "1 pago" or "3 pagos".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
