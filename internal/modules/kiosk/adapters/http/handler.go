// Package http exposes the kiosk use cases over Gin.
package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// Handler serves /kiosk.
type Handler struct{ service *app.Service }

// NewHandler builds the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// Routes mounts the endpoints; every one needs the kiosk module switched on for the gym.
func (h *Handler) Routes(protected gin.IRoutes) {
	guard := auth.RequireFeature(auth.FeatureKiosk)
	protected.GET("/kiosk/settings", guard, h.settings)
	protected.PUT("/kiosk/pin", guard, h.setPIN)
	protected.POST("/kiosk/exit", guard, h.exit)
}

type pinBody struct {
	PIN string `json:"pin"`
}

func (h *Handler) settings(c *gin.Context) {
	p, _ := auth.FromContext(c.Request.Context())
	out, err := h.service.Settings(c.Request.Context(), p)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, out)
}

func (h *Handler) setPIN(c *gin.Context) {
	var in pinBody
	if err := c.ShouldBindJSON(&in); err != nil {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "body", Code: "INVALID_BODY"}))
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	if err := h.service.SetPIN(c.Request.Context(), p, in.PIN); err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, app.Settings{PINSet: true})
}

func (h *Handler) exit(c *gin.Context) {
	var in pinBody
	if err := c.ShouldBindJSON(&in); err != nil {
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "body", Code: "INVALID_BODY"}))
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	if err := h.service.VerifyExit(c.Request.Context(), p, in.PIN); err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, gin.H{"ok": true})
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
	case errors.Is(err, domain.ErrForbidden):
		apierror.Respond(c, apierror.ErrForbidden)
	case errors.Is(err, domain.ErrPINNotSet):
		apierror.Respond(c, apierror.New(http.StatusConflict, "KIOSK_PIN_NOT_SET", "El administrador aún no definió el PIN de salida del quiosco."))
	case errors.Is(err, domain.ErrPINWrong):
		apierror.Respond(c, apierror.New(http.StatusForbidden, "KIOSK_PIN_INVALID", "PIN incorrecto."))
	case errors.Is(err, domain.ErrLocked):
		apierror.Respond(c, apierror.New(http.StatusTooManyRequests, "KIOSK_PIN_LOCKED", "Demasiados intentos. Espera unos minutos."))
	default:
		apierror.Respond(c, err)
	}
}
