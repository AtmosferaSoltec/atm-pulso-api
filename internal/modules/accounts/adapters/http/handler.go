package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Error codes of the accounts module.
const (
	CodeInvalidCredentials        = "INVALID_CREDENTIALS"
	CodeTooManyAttempts           = "TOO_MANY_ATTEMPTS"
	CodeCurrentPasswordIncorrect  = "CURRENT_PASSWORD_INCORRECT"
	codeInvalidBody               = "INVALID_BODY"
	validationFieldForInvalidBody = "body"
)

var (
	errInvalidCredentials = apierror.New(http.StatusUnauthorized, CodeInvalidCredentials, "DNI o contraseña incorrectos.")
	// 400, not 401: the web must not read a wrong current password as "your session ended".
	errCurrentPassword = apierror.New(http.StatusBadRequest, CodeCurrentPasswordIncorrect, "La contraseña actual no es correcta.")
	errTooManyAttempts = apierror.New(http.StatusTooManyRequests, CodeTooManyAttempts, "Demasiados intentos. Inténtalo de nuevo más tarde.")
)

// Handler serves the accounts routes.
type Handler struct {
	service *app.Service
	clock   clock.Clock
}

// NewHandler returns the handler.
func NewHandler(service *app.Service, clock clock.Clock) *Handler {
	return &Handler{service: service, clock: clock}
}

// Routes mounts the module: login behind the strict per-IP limit, the rest protected.
func (h *Handler) Routes(limited, protected gin.IRoutes) {
	limited.POST("/auth/login", h.login)
	protected.GET("/me", h.me)
	protected.PATCH("/me", h.updateMe)
	protected.POST("/me/password", h.changePassword)
}

func (h *Handler) login(c *gin.Context) {
	var req loginRequest
	if !bind(c, &req) {
		return
	}
	session, err := h.service.Login(c.Request.Context(), req.DocumentNumber, req.Password)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, toSessionDTO(session, h.clock.Now()))
}

func (h *Handler) me(c *gin.Context) {
	principal, _ := auth.FromContext(c.Request.Context())
	profile, err := h.service.Me(c.Request.Context(), principal.UserID)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, toUserDTO(profile))
}

func (h *Handler) updateMe(c *gin.Context) {
	var req profileRequest
	if !bind(c, &req) {
		return
	}
	principal, _ := auth.FromContext(c.Request.Context())
	profile, err := h.service.UpdateProfile(c.Request.Context(), principal.UserID, req.FullName, req.Phone)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, toUserDTO(profile))
}

// changePassword answers with a fresh token: the old ones stop working with the change.
func (h *Handler) changePassword(c *gin.Context) {
	var req passwordRequest
	if !bind(c, &req) {
		return
	}
	principal, _ := auth.FromContext(c.Request.Context())
	session, err := h.service.ChangePassword(c.Request.Context(), principal.UserID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, toSessionDTO(session, h.clock.Now()))
}

// bind reads the JSON body, answering the error itself when it cannot.
func bind(c *gin.Context, into any) bool {
	if err := c.ShouldBindJSON(into); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apierror.Respond(c, err)
			return false
		}
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: validationFieldForInvalidBody, Code: codeInvalidBody}))
		return false
	}
	return true
}

// respond translates the module's errors into the API's. Anything unknown goes to
// apierror, which logs it and answers INTERNAL_ERROR.
func respond(c *gin.Context, err error) {
	var validation *domain.ValidationError
	var locked *domain.LockedError
	switch {
	case errors.As(err, &validation):
		fields := make([]apierror.FieldError, 0, len(validation.Issues))
		for _, issue := range validation.Issues {
			fields = append(fields, apierror.FieldError{Field: issue.Field, Code: issue.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.As(err, &locked):
		seconds := int(locked.RetryAfter.Round(time.Second).Seconds())
		if seconds < 1 {
			seconds = 1
		}
		c.Header("Retry-After", strconv.Itoa(seconds))
		apierror.Respond(c, errTooManyAttempts.WithDetails(gin.H{"retry_after_seconds": seconds}))
	case errors.Is(err, domain.ErrInvalidCredentials):
		apierror.Respond(c, errInvalidCredentials)
	case errors.Is(err, domain.ErrCurrentPasswordIncorrect):
		apierror.Respond(c, errCurrentPassword)
	case errors.Is(err, domain.ErrUserNotFound):
		// The token was valid but the user vanished between the check and the lookup.
		apierror.Respond(c, apierror.ErrUnauthorized)
	default:
		apierror.Respond(c, err)
	}
}
