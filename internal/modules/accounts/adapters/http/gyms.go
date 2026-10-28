package http

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/gin-gonic/gin"
)

func (h *Handler) updateGym(c *gin.Context) {
	id, ok := pathID(c, "gym_id")
	if !ok {
		return
	}
	var input app.GymChanges
	if !bind(c, &input) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	gym, err := h.service.UpdateGym(c.Request.Context(), p, id, input)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, gym)
}
func (h *Handler) updateOwner(c *gin.Context)        { h.ownerAction(c, false) }
func (h *Handler) resetOwnerPassword(c *gin.Context) { h.ownerAction(c, true) }
func (h *Handler) ownerAction(c *gin.Context, reset bool) {
	id, ok := pathID(c, "client_id")
	if !ok {
		return
	}
	var input app.UserChanges
	if !reset && !bind(c, &input) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	owner, err := h.service.UpdateOwner(c.Request.Context(), p, id, input, reset)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, owner)
}

// logoUploadTimeout is how long a logo may take to arrive.
const logoUploadTimeout = 90 * time.Second

// setLogo takes the image as the raw request body (PNG, JPEG or WebP); the use case
// checks what it really is.
func (h *Handler) setLogo(c *gin.Context) {
	id, ok := pathID(c, "gym_id")
	if !ok {
		return
	}
	// A file takes longer to arrive than the server's default read timeout allows on a slow link.
	_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now().Add(logoUploadTimeout))
	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, app.MaxLogoBytes+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respond(c, &domain.ValidationError{Issues: []domain.Issue{{Field: "logo", Code: "LOGO_SIZE_INVALID"}}})
			return
		}
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: validationFieldForInvalidBody, Code: codeInvalidBody}))
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	gym, err := h.service.SetLogo(c.Request.Context(), p, id, data)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, gym)
}

func (h *Handler) removeLogo(c *gin.Context) {
	id, ok := pathID(c, "gym_id")
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	gym, err := h.service.RemoveLogo(c.Request.Context(), p, id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, gym)
}

// logo serves a gym's current logo to anyone: it is public branding, and an <img> cannot send
// the bearer token. The bucket itself stays private.
func (h *Handler) logo(c *gin.Context) {
	id, ok := pathID(c, "gym_id")
	if !ok {
		return
	}
	file, cacheable, err := h.service.LogoFile(c.Request.Context(), id, c.Query("v"))
	if err != nil {
		respond(c, err)
		return
	}
	defer func() { _ = file.Body.Close() }()
	cache := "no-cache"
	if cacheable {
		cache = "public, max-age=31536000, immutable"
	}
	c.Header("Cache-Control", cache)
	c.Header("Content-Security-Policy", "default-src 'none'")
	c.Header("Cross-Origin-Resource-Policy", "cross-origin")
	c.DataFromReader(http.StatusOK, file.Size, file.ContentType, file.Body, nil)
}
