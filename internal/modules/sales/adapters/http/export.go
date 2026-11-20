package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/export"
)

// ExportHandler serves GET /sales/export.
type ExportHandler struct {
	service *app.Service
	brands  export.BrandSource
}

// NewExportHandler builds the handler; brands may be nil (Pulso's letterhead).
func NewExportHandler(service *app.Service, brands export.BrandSource) *ExportHandler {
	return &ExportHandler{service: service, brands: brands}
}

// Routes mounts the download behind the sales module guard and the stricter per-IP limit of heavy requests.
func (h *ExportHandler) Routes(protected gin.IRoutes, limit gin.HandlerFunc) {
	protected.GET("/sales/export", auth.RequireFeature(auth.FeatureSales), limit, h.export)
}

var methodLabels = map[string]string{"yape_plin": "Yape/Plin", "cash": "Efectivo", "card": "Tarjeta", "transfer": "Transferencia"}

// saleColumns are the same in the CSV, Excel and the PDF.
var saleColumns = []export.Column{
	{Header: "Fecha", Kind: export.Date, Width: 10},
	{Header: "Hora", Width: 6},
	{Header: "Sede", Width: 13},
	{Header: "Miembro", Width: 22},
	{Header: "Documento", Width: 11},
	{Header: "Productos", Width: 30},
	{Header: "Unidades", Kind: export.Integer, Width: 8},
	{Header: "Total", Kind: export.Money, Width: 11},
	{Header: "Estado", Width: 9},
	{Header: "Medio de pago", Width: 12},
	{Header: "Vendido por", Width: 16},
}

// export answers GET /sales/export?format=csv|xlsx|pdf with the filters of the sales report (from, to, branch_id).
func (h *ExportHandler) export(c *gin.Context) {
	format, ok := export.ParseFormat(c.Query("format"))
	if !ok {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "format", Code: "FORMAT_INVALID"}))
		return
	}
	var filter app.ReportFilter
	from, bad := optionalDate(c, "from")
	if bad {
		return
	}
	to, bad := optionalDate(c, "to")
	if bad {
		return
	}
	if from != nil {
		filter.From = *from
	}
	if to != nil {
		filter.To = *to
	}
	if filter.BranchID, bad = optionalID(c, "branch_id"); bad {
		return
	}
	p := principal(c)
	data, err := h.service.ExportSales(c.Request.Context(), p, filter)
	if errors.Is(err, app.ErrExportTooLarge) {
		apierror.Respond(c, apierror.New(http.StatusRequestEntityTooLarge, "EXPORT_TOO_LARGE", "Hay demasiadas ventas para descargar. Acota las fechas."))
		return
	}
	if err != nil {
		respond(c, err)
		return
	}
	now := h.service.Clock.Now()
	t := data.Totals
	report := export.Report{
		Title: "Ventas de productos", Sheet: "Ventas", GeneratedAt: now,
		Columns: saleColumns, Rows: saleRows(data.Sales),
		Filters: saleFilters(data),
		Figures: []export.Figure{
			{Label: "Ventas", Value: strconv.Itoa(t.SalesCount)},
			{Label: "Unidades", Value: strconv.Itoa(t.Units)},
			{Label: "Total vendido", Value: export.FormatMoney(t.TotalSold)},
			{Label: "Cobrado", Value: export.FormatMoney(t.Collected)},
			{Label: "Pendiente de cobro", Value: export.FormatMoney(t.Pending)},
		},
		TotalRow: []string{"", "", "Total", salesCount(t.SalesCount), "", "", strconv.Itoa(t.Units), t.TotalSold, "", "", ""},
	}
	if format == export.PDF {
		report.Brand = export.BrandOf(c.Request.Context(), h.brands, p.GymID, p.TenantID)
	}
	body, err := export.Render(format, report)
	if err != nil {
		apierror.Respond(c, err)
		return
	}
	c.Header("Content-Disposition", export.Disposition(export.Filename("ventas", clock.LimaDate(now), format)))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, format.ContentType(), body)
}

func saleRows(sales []app.SaleView) [][]string {
	out := make([][]string, 0, len(sales))
	for _, s := range sales {
		hour := ""
		if at, err := time.Parse(time.RFC3339, s.SoldAt); err == nil {
			hour = at.In(export.Lima).Format("15:04")
		}
		member, document := "", ""
		if s.Member != nil {
			member, document = s.Member.FullName, s.Member.DocumentNumber
		}
		items := make([]string, 0, len(s.Items))
		for _, item := range s.Items {
			items = append(items, strconv.Itoa(item.Quantity)+" × "+item.Name)
		}
		status := "Pagado"
		if s.Status == "pending" {
			status = "Pendiente"
		}
		method := ""
		if s.Method != nil {
			method = methodLabels[*s.Method]
		}
		out = append(out, []string{s.SoldOn, hour, s.Branch.Name, member, document, strings.Join(items, ", "),
			strconv.Itoa(s.Units), s.Total, status, method, s.SoldBy.FullName})
	}
	return out
}

func saleFilters(data app.SalesExport) []export.Filter {
	period := data.From.Format("02/01/2006")
	if !data.From.Equal(data.To) {
		period += " – " + data.To.Format("02/01/2006")
	}
	branch := "Todas"
	if data.BranchName != "" {
		branch = data.BranchName
	}
	return []export.Filter{{Label: "Período", Value: period}, {Label: "Sede", Value: branch}}
}

// salesCount is "1 venta" or "3 ventas".
func salesCount(n int) string {
	if n == 1 {
		return "1 venta"
	}
	return strconv.Itoa(n) + " ventas"
}
