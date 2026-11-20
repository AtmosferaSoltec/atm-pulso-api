package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/export"
)

// ExportHandler serves GET /members/export.
type ExportHandler struct {
	service *app.Service
	brands  export.BrandSource
}

// NewExportHandler builds the handler; brands may be nil (Pulso's letterhead).
func NewExportHandler(service *app.Service, brands export.BrandSource) *ExportHandler {
	return &ExportHandler{service: service, brands: brands}
}

// Routes mounts the download behind the stricter per-IP limit of heavy requests.
func (h *ExportHandler) Routes(protected gin.IRoutes, limit gin.HandlerFunc) {
	protected.GET("/members/export", limit, h.export)
}

var statusLabels = map[string]string{"active": "Vigente", "expiring": "Por vencer", "expired": "Vencido", "scheduled": "Por iniciar"}

var documentLabels = map[string]string{"dni": "DNI", "ce": "CE"}

// memberColumns are the same in the CSV, Excel and the PDF.
var memberColumns = []export.Column{
	{Header: "Tipo de documento", Width: 8},
	{Header: "Documento", Width: 12},
	{Header: "Nombre", Width: 30},
	{Header: "Teléfono", Width: 12},
	{Header: "Sede", Width: 15},
	{Header: "Estado", Width: 11},
	{Header: "Plan", Width: 18},
	{Header: "Inicio", Kind: export.Date, Width: 11},
	{Header: "Fin", Kind: export.Date, Width: 11},
	{Header: "Días restantes", Kind: export.Integer, Width: 9},
	{Header: "Última boleta", Width: 12},
}

// export answers GET /members/export?format=csv|xlsx|pdf with the filters of the list (q, status, home_branch_id).
func (h *ExportHandler) export(c *gin.Context) {
	format, ok := export.ParseFormat(c.Query("format"))
	if !ok {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "format", Code: "FORMAT_INVALID"}))
		return
	}
	filter := app.Filter{Query: c.Query("q"), Status: c.Query("status")}
	if !statuses[filter.Status] {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "status", Code: "STATUS_INVALID"}))
		return
	}
	if raw := c.Query("home_branch_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "home_branch_id", Code: "BRANCH_INVALID"}))
			return
		}
		filter.HomeBranchID = &id
	}
	p, _ := auth.FromContext(c.Request.Context())
	data, err := h.service.Export(c.Request.Context(), p, filter)
	switch {
	case errors.Is(err, app.ErrExportTooLarge):
		apierror.Respond(c, apierror.New(http.StatusRequestEntityTooLarge, "EXPORT_TOO_LARGE", "Hay demasiados miembros para descargar. Usa un filtro."))
		return
	case errors.Is(err, branchdir.ErrNotFound):
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "home_branch_id", Code: "BRANCH_INVALID"}))
		return
	case err != nil:
		respond(c, err)
		return
	}
	now := h.service.Clock.Now()
	report := export.Report{
		Title: "Miembros", Sheet: "Miembros", GeneratedAt: now,
		Columns: memberColumns, Rows: memberRows(data.Items),
		Filters: memberFilters(filter, data),
		Figures: []export.Figure{
			{Label: "Miembros", Value: strconv.Itoa(len(data.Items))},
			{Label: "Vigentes", Value: strconv.Itoa(data.Counts.Active)},
			{Label: "Por vencer", Value: strconv.Itoa(data.Counts.Expiring)},
			{Label: "Vencidos", Value: strconv.Itoa(data.Counts.Expired)},
			{Label: "Por iniciar", Value: strconv.Itoa(data.Counts.Scheduled)},
		},
	}
	if format == export.PDF {
		report.Brand = export.BrandOf(c.Request.Context(), h.brands, p.GymID, p.TenantID)
	}
	body, err := export.Render(format, report)
	if err != nil {
		apierror.Respond(c, err)
		return
	}
	c.Header("Content-Disposition", export.Disposition(export.Filename("miembros", clock.LimaDate(now), format)))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, format.ContentType(), body)
}

func memberRows(items []app.MemberView) [][]string {
	out := make([][]string, 0, len(items))
	for _, m := range items {
		plan := strings.Join(m.PlanNames, ", ")
		if plan == "" {
			plan = m.PlanLabel
		}
		receipt := ""
		if m.LastReceipt != nil {
			receipt = *m.LastReceipt
		}
		docType := documentLabels[m.DocumentType]
		if docType == "" {
			docType = strings.ToUpper(m.DocumentType)
		}
		out = append(out, []string{docType, m.DocumentNumber, m.FullName, m.Phone, m.HomeBranch.Name, statusLabels[m.Status],
			plan, m.StartsOn, m.EndsOn, strconv.Itoa(max(m.DaysRemaining, 0)), receipt})
	}
	return out
}

func memberFilters(f app.Filter, data app.MemberExport) []export.Filter {
	status := "Todos"
	if f.Status != "" {
		status = statusLabels[f.Status]
	}
	filters := []export.Filter{{Label: "Estado", Value: status}}
	if data.HomeBranchName != "" {
		filters = append(filters, export.Filter{Label: "Sede", Value: data.HomeBranchName})
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		filters = append(filters, export.Filter{Label: "Búsqueda", Value: q})
	}
	return filters
}
