// Package http exposes the reports over Gin.
package http

import (
	"bytes"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/reports/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// Handler serves /payments and /dashboard.
type Handler struct{ service *app.Service }

// NewHandler builds the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// Routes mounts the endpoints. exportLimit throttles the heavy CSV like the login.
func (h *Handler) Routes(protected gin.IRoutes, scope gin.HandlerFunc, exportLimit gin.HandlerFunc) {
	protected.GET("/payments", scope, h.payments)
	protected.GET("/payments/export", scope, exportLimit, h.export)
	protected.GET("/dashboard", scope, h.dashboard)
}

var kinds = map[string]bool{"": true, "membership": true, "day_pass": true}

func parseQuery(c *gin.Context) (app.PaymentQuery, bool) {
	q := app.PaymentQuery{Method: c.Query("method"), Kind: c.Query("kind"), Query: c.Query("q"), Page: 1, PageSize: 25}
	fail := func(field, code string) (app.PaymentQuery, bool) {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: field, Code: code}))
		return q, false
	}
	if !app.ValidMethod(q.Method) {
		return fail("method", "METHOD_INVALID")
	}
	if !kinds[q.Kind] {
		return fail("kind", "KIND_INVALID")
	}
	var err error
	if q.BranchID, err = optionalID(c, "branch_id"); err != nil {
		return fail("branch_id", "ID_INVALID")
	}
	if q.GymID, err = optionalID(c, "gym_id"); err != nil {
		return fail("gym_id", "ID_INVALID")
	}
	if q.From, err = optionalDate(c, "from"); err != nil {
		return fail("from", "DATE_INVALID")
	}
	if q.To, err = optionalDate(c, "to"); err != nil {
		return fail("to", "DATE_INVALID")
	}
	for _, field := range []string{"page", "page_size"} {
		raw := c.Query(field)
		if raw == "" {
			continue
		}
		value, convErr := strconv.Atoi(raw)
		if convErr != nil || value < 1 || (field == "page_size" && value > 100) || value > 1_000_000 {
			return fail(field, "PAGINATION_INVALID")
		}
		if field == "page" {
			q.Page = value
		} else {
			q.PageSize = value
		}
	}
	return q, true
}

func optionalID(c *gin.Context, name string) (*uuid.UUID, error) {
	raw := c.Query(name)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	return &id, err
}

func optionalDate(c *gin.Context, name string) (*time.Time, error) {
	raw := c.Query(name)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	return &t, err
}

func (h *Handler) payments(c *gin.Context) {
	q, ok := parseQuery(c)
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	rows, total, totals, err := h.service.Payments(c.Request.Context(), p, q)
	if err != nil {
		respond(c, err)
		return
	}
	meta := apierror.Meta{Page: q.Page, PageSize: q.PageSize, Total: int64(total)}
	if totals != nil {
		meta.Totals = totals
	}
	apierror.Page(c, rows, meta)
}

var methodLabels = map[string]string{"yape_plin": "Yape/Plin", "cash": "Efectivo", "card": "Tarjeta", "transfer": "Transferencia"}

// safeCell stops spreadsheet formulas: a cell starting with = + - or @ is prefixed with a quote.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (h *Handler) export(c *gin.Context) {
	q, ok := parseQuery(c)
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	rows, err := h.service.Export(c.Request.Context(), p, q)
	if err != nil {
		respond(c, err)
		return
	}
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"Boleta", "Miembro", "Documento", "Plan", "Importe", "Medio de pago", "Fecha"})
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
		_ = w.Write([]string{row.Receipt.Number, safeCell(name), safeCell(document), row.PlanLabel, row.Amount, methodLabels[row.Method], row.PaidOn})
	}
	w.Flush()
	c.Header("Content-Disposition", `attachment; filename="pagos.csv"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

func (h *Handler) dashboard(c *gin.Context) {
	branch, err1 := optionalID(c, "branch_id")
	gym, err2 := optionalID(c, "gym_id")
	if err1 != nil || err2 != nil {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "branch_id", Code: "ID_INVALID"}))
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	view, err := h.service.Dashboard(c.Request.Context(), p, branch, gym)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, view)
}

func respond(c *gin.Context, err error) {
	switch {
	case errors.Is(err, app.ErrForbidden):
		apierror.Respond(c, apierror.ErrForbidden)
	case errors.Is(err, app.ErrBranchNotAccessible):
		apierror.Respond(c, apierror.New(http.StatusForbidden, "BRANCH_NOT_ACCESSIBLE", "No tienes acceso a esta sede."))
	case errors.Is(err, app.ErrExportTooLarge):
		apierror.Respond(c, apierror.New(http.StatusRequestEntityTooLarge, "EXPORT_TOO_LARGE", "Hay demasiados pagos para descargar. Acota las fechas."))
	case errors.Is(err, app.ErrRange):
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "from", Code: "RANGE_INVALID"}))
	default:
		apierror.Respond(c, err)
	}
}
