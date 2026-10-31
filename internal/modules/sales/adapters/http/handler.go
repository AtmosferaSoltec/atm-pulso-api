// Package http exposes the products use cases over Gin. Every route sits behind the module guard.
package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
)

// Handler serves /products, /sales and /members/{id}/pending-sales.
type Handler struct{ service *app.Service }

// NewHandler builds the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// Routes mounts the endpoints; each one requires the products module of the gym.
func (h *Handler) Routes(protected gin.IRoutes) {
	on := auth.RequireFeature(auth.FeatureSales)
	protected.GET("/products", on, h.products)
	protected.POST("/products", on, h.createProduct)
	protected.PATCH("/products/:id", on, h.updateProduct)
	protected.GET("/sales", on, h.sales)
	protected.POST("/sales", on, h.sell)
	protected.GET("/sales/pending", on, h.pendingBalances)
	protected.GET("/sales/report", on, h.report)
	protected.POST("/sales/collections", on, h.collect)
	protected.GET("/members/:id/pending-sales", on, h.memberPending)
	protected.GET("/members/:id/sales", on, h.memberSales)
}

func principal(c *gin.Context) auth.Principal {
	p, _ := auth.FromContext(c.Request.Context())
	return p
}

func (h *Handler) products(c *gin.Context) {
	list, err := h.service.Products(c.Request.Context(), principal(c), c.Query("include_inactive") == "true")
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, list)
}

func (h *Handler) createProduct(c *gin.Context) {
	var in app.ProductInput
	if !bind(c, &in) {
		return
	}
	product, existed, err := h.service.CreateProduct(c.Request.Context(), principal(c), in)
	if err != nil {
		respond(c, err)
		return
	}
	if existed {
		apierror.OK(c, product)
		return
	}
	apierror.Created(c, product)
}

func (h *Handler) updateProduct(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in app.ProductChanges
	if !bind(c, &in) {
		return
	}
	product, err := h.service.UpdateProduct(c.Request.Context(), principal(c), id, in)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, product)
}

func (h *Handler) sell(c *gin.Context) {
	var in app.SaleInput
	if !bind(c, &in) {
		return
	}
	sale, existed, err := h.service.Sell(c.Request.Context(), principal(c), in)
	if err != nil {
		respond(c, err)
		return
	}
	if existed {
		apierror.OK(c, sale)
		return
	}
	apierror.Created(c, sale)
}

func (h *Handler) sales(c *gin.Context) {
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	filter := app.SaleFilter{Page: page, PageSize: size, Status: c.Query("status")}
	if filter.Status != "" && filter.Status != domain.StatusPaid && filter.Status != domain.StatusPending {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "status", Code: domain.CodeStatus}))
		return
	}
	var bad bool
	if filter.From, bad = optionalDate(c, "from"); bad {
		return
	}
	if filter.To, bad = optionalDate(c, "to"); bad {
		return
	}
	branchID, bad := optionalID(c, "branch_id")
	if bad {
		return
	}
	if filter.MemberID, bad = optionalID(c, "member_id"); bad {
		return
	}
	list, total, err := h.service.Sales(c.Request.Context(), principal(c), filter, branchID)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Page(c, list, apierror.Meta{Page: page, PageSize: size, Total: int64(total)})
}

func (h *Handler) pendingBalances(c *gin.Context) {
	list, err := h.service.PendingBalances(c.Request.Context(), principal(c))
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, list)
}

func (h *Handler) memberSales(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	list, total, err := h.service.MemberSales(c.Request.Context(), principal(c), id, page, size)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Page(c, list, apierror.Meta{Page: page, PageSize: size, Total: int64(total)})
}

func (h *Handler) memberPending(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	pending, err := h.service.MemberPending(c.Request.Context(), principal(c), id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, pending)
}

func (h *Handler) collect(c *gin.Context) {
	var in app.CollectInput
	if !bind(c, &in) {
		return
	}
	collection, existed, err := h.service.Collect(c.Request.Context(), principal(c), in)
	if err != nil {
		respond(c, err)
		return
	}
	if existed {
		apierror.OK(c, collection)
		return
	}
	apierror.Created(c, collection)
}

func (h *Handler) report(c *gin.Context) {
	filter := app.ReportFilter{GroupBy: c.Query("group_by")}
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
	branchID, bad := optionalID(c, "branch_id")
	if bad {
		return
	}
	filter.BranchID = branchID
	report, err := h.service.Report(c.Request.Context(), principal(c), filter)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, report)
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

// optionalDate reads a YYYY-MM-DD query parameter; bad is true once it has answered 400.
func optionalDate(c *gin.Context, field string) (*time.Time, bool) {
	raw := c.Query(field)
	if raw == "" {
		return nil, false
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: field, Code: domain.CodeDate}))
		return nil, true
	}
	return &t, false
}

func optionalID(c *gin.Context, field string) (*uuid.UUID, bool) {
	raw := c.Query(field)
	if raw == "" {
		return nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: field, Code: "ID_INVALID"}))
		return nil, true
	}
	return &id, false
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
	case errors.Is(err, domain.ErrProductNotFound):
		apierror.Respond(c, apierror.New(http.StatusNotFound, "PRODUCT_NOT_FOUND", "El producto ya no existe."))
	case errors.Is(err, domain.ErrProductNameTaken):
		apierror.Respond(c, apierror.New(http.StatusConflict, "PRODUCT_NAME_TAKEN", "Ya tienes un producto con ese nombre."))
	case errors.Is(err, domain.ErrSaleNotFound):
		apierror.Respond(c, apierror.New(http.StatusNotFound, "SALE_NOT_FOUND", "La venta ya no existe."))
	case errors.Is(err, domain.ErrSaleNotPending):
		apierror.Respond(c, apierror.New(http.StatusConflict, "SALE_NOT_PENDING", "Alguna venta ya fue saldada. Actualiza la lista."))
	case errors.Is(err, domain.ErrNothingToCollect):
		apierror.Respond(c, apierror.New(http.StatusConflict, "NOTHING_TO_COLLECT", "El miembro no tiene deuda pendiente."))
	case errors.Is(err, domain.ErrMemberNotFound):
		apierror.Respond(c, apierror.New(http.StatusNotFound, "MEMBER_NOT_FOUND", "El miembro ya no existe."))
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
