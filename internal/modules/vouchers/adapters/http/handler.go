// Package http exposes the vouchers use cases over Gin.
package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// uploadTimeout is how long a voucher may take to arrive, over the server's default read timeout.
const uploadTimeout = 60 * time.Second

// Handler serves the vouchers of receipts, product sales and collections, and their files.
type Handler struct{ service *app.Service }

// NewHandler builds the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// UploadRoutes are the upload paths (relative to the API prefix): each needs a body limit of domain.MaxBytes+1.
var UploadRoutes = []string{"/receipts/:id/vouchers", "/sales/:id/vouchers", "/sales/collections/:id/vouchers"}

// lister and uploader are the use cases of one kind of owner, addressed by the id in the path.
type (
	lister   func(ctx context.Context, p auth.Principal, id uuid.UUID) ([]app.Voucher, error)
	uploader func(ctx context.Context, p auth.Principal, id uuid.UUID, data []byte) (app.Voucher, error)
)

// Routes mounts the endpoints. Those of product sales need the gym's sales module.
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/receipts/:id/vouchers", h.list(h.service.List))
	protected.POST("/receipts/:id/vouchers", h.upload(h.service.Upload))
	sales := auth.RequireFeature(auth.FeatureSales)
	protected.GET("/sales/:id/vouchers", sales, h.list(h.service.ListForSale))
	protected.POST("/sales/:id/vouchers", sales, h.upload(h.service.UploadForSale))
	protected.GET("/sales/collections/:id/vouchers", sales, h.list(h.service.ListForCollection))
	protected.POST("/sales/collections/:id/vouchers", sales, h.upload(h.service.UploadForCollection))
	protected.GET("/vouchers/:id/file", h.file)
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) list(use lister) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c)
		if !ok {
			return
		}
		p, _ := auth.FromContext(c.Request.Context())
		vouchers, err := use(c.Request.Context(), p, id)
		if err != nil {
			respond(c, err)
			return
		}
		apierror.OK(c, vouchers)
	}
}

// upload takes the file as the raw request body (PNG, JPEG, WebP or PDF); the use case checks what it really is.
func (h *Handler) upload(use uploader) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c)
		if !ok {
			return
		}
		_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now().Add(uploadTimeout))
		data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, domain.MaxBytes+1))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				respond(c, &domain.ValidationError{Issues: []domain.Issue{{Field: "voucher", Code: domain.CodeSize}}})
				return
			}
			apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "body", Code: "INVALID_BODY"}))
			return
		}
		p, _ := auth.FromContext(c.Request.Context())
		voucher, err := use(c.Request.Context(), p, id, data)
		if err != nil {
			respond(c, err)
			return
		}
		apierror.Created(c, voucher)
	}
}

// file streams the voucher. It is private: only with a valid token, and never cached by shared caches.
func (h *Handler) file(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	voucher, file, err := h.service.Open(c.Request.Context(), p, id)
	if err != nil {
		respond(c, err)
		return
	}
	defer func() { _ = file.Body.Close() }()
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("Content-Security-Policy", "default-src 'none'")
	c.DataFromReader(http.StatusOK, file.Size, voucher.ContentType, file.Body, nil)
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
	case errors.Is(err, domain.ErrReceiptNotFound), errors.Is(err, domain.ErrVoucherNotFound),
		errors.Is(err, domain.ErrSaleNotFound), errors.Is(err, domain.ErrCollectionNotFound):
		apierror.Respond(c, apierror.ErrNotFound)
	case errors.Is(err, domain.ErrSaleNotPaid):
		apierror.Respond(c, apierror.New(http.StatusConflict, "VOUCHER_SALE_NOT_PAID", "La venta aún no está pagada: adjunta el comprobante al cobrarla."))
	case errors.Is(err, domain.ErrSaleCollected):
		apierror.Respond(c, apierror.New(http.StatusConflict, "VOUCHER_SALE_COLLECTED", "Esta venta se pagó en un cobro: adjunta el comprobante a ese cobro."))
	case errors.Is(err, domain.ErrFeatureDisabled):
		apierror.Respond(c, apierror.New(http.StatusForbidden, auth.CodeFeatureDisabled, "Esta función no está activada para tu gimnasio."))
	case errors.Is(err, domain.ErrForbidden):
		apierror.Respond(c, apierror.ErrForbidden)
	case errors.Is(err, domain.ErrLimitReached):
		apierror.Respond(c, apierror.New(http.StatusConflict, "VOUCHER_LIMIT_REACHED", "Este pago ya tiene el máximo de comprobantes."))
	case errors.Is(err, domain.ErrStorageUnavailable):
		apierror.Respond(c, apierror.New(http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "El almacenamiento de imágenes no está disponible."))
	default:
		apierror.Respond(c, err)
	}
}
