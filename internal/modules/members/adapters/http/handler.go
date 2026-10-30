// Package http exposes the members use cases over Gin.
package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
)

// Handler serves /members and /receipts.
type Handler struct{ service *app.Service }

// NewHandler builds the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// Routes mounts the endpoints.
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/members", h.list)
	protected.POST("/members", h.enroll)
	protected.GET("/members/:id", h.get)
	protected.PATCH("/members/:id", h.update)
	protected.POST("/members/:id/renewals", h.renew)
	protected.GET("/members/:id/receipts", h.memberReceipts)
	protected.GET("/receipts/:id", h.receipt)
}

var statuses = map[string]bool{"": true, "active": true, "expiring": true, "expired": true, "scheduled": true}

func (h *Handler) list(c *gin.Context) {
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	filter := app.Filter{Query: c.Query("q"), Status: c.Query("status"), Page: page, PageSize: size}
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
	result, err := h.service.List(c.Request.Context(), p, filter)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Page(c, result.Items, apierror.Meta{Page: page, PageSize: size, Total: int64(result.Total), Counts: result.Counts})
}

func (h *Handler) enroll(c *gin.Context) {
	var in app.EnrollInput
	if !bind(c, &in) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	sale, existed, err := h.service.Enroll(c.Request.Context(), p, in)
	if err != nil {
		respond(c, err)
		return
	}
	answer(c, sale, existed)
}

func (h *Handler) renew(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in app.RenewInput
	if !bind(c, &in) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	sale, existed, err := h.service.Renew(c.Request.Context(), p, id, in)
	if err != nil {
		respond(c, err)
		return
	}
	answer(c, sale, existed)
}

func answer(c *gin.Context, sale app.Sale, existed bool) {
	if existed {
		apierror.OK(c, sale)
		return
	}
	apierror.Created(c, sale)
}

func (h *Handler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	member, err := h.service.Get(c.Request.Context(), p, id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, member)
}

func (h *Handler) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in struct {
		FullName *string `json:"full_name"`
		Phone    *string `json:"phone"`
	}
	if !bind(c, &in) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	member, err := h.service.Update(c.Request.Context(), p, id, in.FullName, in.Phone)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, member)
}

func (h *Handler) memberReceipts(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	list, err := h.service.Receipts(c.Request.Context(), p, id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, list)
}

func (h *Handler) receipt(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	receipt, err := h.service.Receipt(c.Request.Context(), p, id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, receipt)
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

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
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
	var taken *domain.DocumentTakenError
	switch {
	case errors.As(err, &validation):
		fields := make([]apierror.FieldError, 0, len(validation.Issues))
		for _, issue := range validation.Issues {
			fields = append(fields, apierror.FieldError{Field: issue.Field, Code: issue.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.As(err, &taken):
		apierror.Respond(c, apierror.New(http.StatusConflict, "MEMBER_DOCUMENT_TAKEN", "Este documento ya pertenece a un miembro.").
			WithDetails(gin.H{"member_id": taken.MemberID}))
	case errors.Is(err, domain.ErrMemberNotFound):
		apierror.Respond(c, apierror.New(http.StatusNotFound, "MEMBER_NOT_FOUND", "El miembro ya no existe."))
	case errors.Is(err, domain.ErrReceiptNotFound):
		apierror.Respond(c, apierror.ErrNotFound)
	case errors.Is(err, branchdir.ErrNotFound):
		apierror.Respond(c, apierror.New(http.StatusBadRequest, "SCOPE_NOT_ALLOWED", "La sede no está disponible."))
	case errors.Is(err, domain.ErrBranchNotAccessible):
		apierror.Respond(c, apierror.New(http.StatusForbidden, "BRANCH_NOT_ACCESSIBLE", "No tienes acceso a esta sede."))
	case errors.Is(err, domain.ErrAccessBranches):
		apierror.Respond(c, apierror.New(http.StatusBadRequest, "ACCESS_BRANCHES_INVALID", "Las sedes de acceso no son válidas."))
	case errors.Is(err, domain.ErrForbidden):
		apierror.Respond(c, apierror.ErrForbidden)
	case errors.Is(err, domain.ErrConflict):
		apierror.Respond(c, apierror.New(http.StatusConflict, "CONFLICT", "El registro ya existe."))
	default:
		apierror.Respond(c, err)
	}
}
