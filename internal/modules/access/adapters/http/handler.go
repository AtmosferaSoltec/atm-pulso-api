// Package http exposes the access use cases over Gin.
package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
)

// Handler serves /access and /day-passes.
type Handler struct{ service *app.Service }

// NewHandler builds the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// Routes mounts the endpoints; scope applies the organization (or superadmin inspection) scope.
func (h *Handler) Routes(protected gin.IRoutes, scope gin.HandlerFunc) {
	protected.POST("/access/checks", scope, h.check)
	protected.GET("/access/events", scope, h.events)
	protected.GET("/access/summary", scope, h.summary)
	protected.GET("/day-passes", scope, h.listDayPasses)
	protected.POST("/day-passes", scope, h.sellDayPass)
	protected.GET("/day-passes/:id", scope, h.getDayPass)
}

func (h *Handler) check(c *gin.Context) {
	var in app.CheckInput
	if !bind(c, &in) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	result, err := h.service.Check(c.Request.Context(), p, in)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, result)
}

func (h *Handler) events(c *gin.Context) {
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	q := app.EventsQuery{Query: c.Query("q"), Result: c.Query("result"), Page: page, PageSize: size}
	if q.Result != "" && q.Result != "allowed" && q.Result != "denied" {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "result", Code: "RESULT_INVALID"}))
		return
	}
	var ok1, ok2, ok3 bool
	if q.BranchID, ok1 = optionalID(c, "branch_id"); !ok1 {
		return
	}
	if q.From, ok2 = optionalDate(c, "from"); !ok2 {
		return
	}
	if q.To, ok3 = optionalDate(c, "to"); !ok3 {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	items, total, err := h.service.Events(c.Request.Context(), p, q)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Page(c, items, apierror.Meta{Page: page, PageSize: size, Total: int64(total)})
}

func (h *Handler) summary(c *gin.Context) {
	branch, ok := optionalID(c, "branch_id")
	if !ok {
		return
	}
	date, ok := optionalDate(c, "date")
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	summary, err := h.service.SummaryOf(c.Request.Context(), p, branch, date)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, summary)
}

func (h *Handler) sellDayPass(c *gin.Context) {
	var in app.DayPassInput
	if !bind(c, &in) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	view, existed, err := h.service.SellDayPass(c.Request.Context(), p, in)
	if err != nil {
		respond(c, err)
		return
	}
	if existed {
		apierror.OK(c, view)
		return
	}
	apierror.Created(c, view)
}

func (h *Handler) getDayPass(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	view, err := h.service.DayPass(c.Request.Context(), p, id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, view)
}

func (h *Handler) listDayPasses(c *gin.Context) {
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	q := app.DayPassQuery{Page: page, PageSize: size}
	var ok1, ok2, ok3 bool
	if q.BranchID, ok1 = optionalID(c, "branch_id"); !ok1 {
		return
	}
	if q.From, ok2 = optionalDate(c, "from"); !ok2 {
		return
	}
	if q.To, ok3 = optionalDate(c, "to"); !ok3 {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	items, total, err := h.service.DayPasses(c.Request.Context(), p, q)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Page(c, items, apierror.Meta{Page: page, PageSize: size, Total: int64(total)})
}

func optionalID(c *gin.Context, name string) (*uuid.UUID, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: name, Code: "ID_INVALID"}))
		return nil, false
	}
	return &id, true
}

func optionalDate(c *gin.Context, name string) (*time.Time, bool) {
	raw := c.Query(name)
	if raw == "" {
		return nil, true
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: name, Code: "DATE_INVALID"}))
		return nil, false
	}
	return &t, true
}

func bind(c *gin.Context, into any) bool {
	if err := c.ShouldBindJSON(into); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierror.Respond(c, err)
			return false
		}
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "body", Code: "INVALID_BODY"}))
		return false
	}
	return true
}

func pagination(c *gin.Context) (int, int, bool) {
	page, size := 1, 25
	for _, field := range []string{"page", "page_size"} {
		raw := c.Query(field)
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || (field == "page_size" && value > 100) || value > 1_000_000 {
			apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: field, Code: "PAGINATION_INVALID"}))
			return 0, 0, false
		}
		if field == "page" {
			page = value
		} else {
			size = value
		}
	}
	return page, size, true
}

func respond(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		fields := make([]apierror.FieldError, 0, len(validation.Issues))
		for _, issue := range validation.Issues {
			fields = append(fields, apierror.FieldError{Field: issue.Field, Code: issue.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.Is(err, domain.ErrMemberNotFound):
		apierror.Respond(c, apierror.New(http.StatusNotFound, "MEMBER_NOT_FOUND", "El miembro no existe."))
	case errors.Is(err, domain.ErrNotFound):
		apierror.Respond(c, apierror.ErrNotFound)
	case errors.Is(err, branchdir.ErrNotFound):
		apierror.Respond(c, apierror.New(http.StatusBadRequest, "SCOPE_NOT_ALLOWED", "La sede no está disponible."))
	case errors.Is(err, domain.ErrBranchNotAccessible):
		apierror.Respond(c, apierror.New(http.StatusForbidden, "BRANCH_NOT_ACCESSIBLE", "No tienes acceso a esta sede."))
	case errors.Is(err, domain.ErrForbidden):
		apierror.Respond(c, apierror.ErrForbidden)
	case errors.Is(err, domain.ErrConflict):
		apierror.Respond(c, apierror.New(http.StatusConflict, "CONFLICT", "El registro ya existe."))
	default:
		apierror.Respond(c, err)
	}
}
