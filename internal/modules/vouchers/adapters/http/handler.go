// Package http exposes the vouchers use cases over Gin.
package http

import (
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

// Handler serves the vouchers of a receipt and their files.
type Handler struct{ service *app.Service }

// NewHandler builds the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// Routes mounts the endpoints.
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/receipts/:id/vouchers", h.list)
	protected.POST("/receipts/:id/vouchers", h.upload)
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

func (h *Handler) list(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	vouchers, err := h.service.List(c.Request.Context(), p, id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, vouchers)
}

// upload takes the file as the raw request body (PNG, JPEG, WebP or PDF); the use case checks what it really is.
func (h *Handler) upload(c *gin.Context) {
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
	voucher, err := h.service.Upload(c.Request.Context(), p, id, data)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Created(c, voucher)
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
	case errors.Is(err, domain.ErrReceiptNotFound), errors.Is(err, domain.ErrVoucherNotFound):
		apierror.Respond(c, apierror.ErrNotFound)
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
