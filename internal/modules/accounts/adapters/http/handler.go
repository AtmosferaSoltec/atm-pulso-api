// Package http is the accounts module's HTTP adapter: it translates requests into use
// cases and answers with the API's JSON shape. No business rules live here.
package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

const (
	codeInvalidBody               = "INVALID_BODY"
	validationFieldForInvalidBody = "body"
)

// Handler serves the accounts routes.
type Handler struct {
	service *app.Service
}

// NewHandler returns the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// Routes mounts the routes of people who work in a gym (behind auth.Guard.User).
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/me", h.me)
	protected.PATCH("/me", h.updateMe)
	protected.GET("/catalog", h.catalog)
	protected.PATCH("/gyms/:gym_id", h.updateGym)
	protected.GET("/users", h.listUsers)
	protected.POST("/users", h.createUser)
	protected.PATCH("/users/:id", h.updateUser)
	protected.POST("/users/:id/activate", h.activateUser)
	protected.POST("/users/:id/deactivate", h.deactivateUser)
	protected.POST("/users/:id/password", h.resetUserPassword)
}

func (h *Handler) me(c *gin.Context) {
	principal, _ := auth.FromContext(c.Request.Context())
	profile, err := h.service.Me(c.Request.Context(), principal)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, toUserDTO(profile, principal.Role))
}

func (h *Handler) updateMe(c *gin.Context) {
	var req profileRequest
	if !bind(c, &req) {
		return
	}
	principal, _ := auth.FromContext(c.Request.Context())
	profile, err := h.service.UpdateProfile(c.Request.Context(), principal, req.FullName, req.Phone)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, toUserDTO(profile, principal.Role))
}

// catalog returns the shared directory and the current user's branch access.
func (h *Handler) catalog(c *gin.Context) {
	principal, _ := auth.FromContext(c.Request.Context())
	catalog, err := h.service.Catalog(c.Request.Context(), principal)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, catalog)
}

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

func pathID(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil || id == uuid.Nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
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
	switch {
	case errors.Is(err, domain.ErrCannotModifySelf):
		apierror.Respond(c, apierror.New(http.StatusForbidden, "CANNOT_MODIFY_SELF", "No puedes modificar tu propia cuenta aquí."))
	case errors.Is(err, domain.ErrRoleNotAssignable):
		apierror.Respond(c, apierror.New(http.StatusForbidden, "ROLE_NOT_ASSIGNABLE", "No puedes asignar este rol."))
	case errors.Is(err, domain.ErrScopeNotAllowed):
		apierror.Respond(c, apierror.New(http.StatusForbidden, "SCOPE_NOT_ALLOWED", "No puedes administrar este gimnasio o sede."))
	case errors.Is(err, domain.ErrForeignScope):
		apierror.Respond(c, apierror.New(http.StatusBadRequest, "SCOPE_NOT_ALLOWED", "El gimnasio o sede no está disponible."))
	case errors.Is(err, domain.ErrForbidden):
		apierror.Respond(c, apierror.ErrForbidden)
	case errors.Is(err, domain.ErrConflict):
		apierror.Respond(c, apierror.New(http.StatusConflict, "CONFLICT", "El registro ya existe."))
	case errors.Is(err, domain.ErrDocumentTaken):
		apierror.Respond(c, apierror.New(http.StatusConflict, "USER_DOCUMENT_TAKEN", "Este DNI ya pertenece a un usuario."))
	case errors.Is(err, domain.ErrUserInOtherCompany):
		apierror.Respond(c, apierror.New(http.StatusConflict, "USER_IN_OTHER_COMPANY", "Esta persona ya trabaja para otra empresa. Pide a Atmósfera que le dé acceso."))
	case errors.Is(err, domain.ErrBranchInUse):
		apierror.Respond(c, apierror.New(http.StatusConflict, "BRANCH_IN_USE", "La sede tiene movimientos o personas, o es la única del gimnasio."))
	case errors.Is(err, domain.ErrAuthUnavailable):
		apierror.Respond(c, apierror.New(http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "No pudimos conectar con el servicio de cuentas. Inténtalo en un momento."))
	case errors.As(err, &validation):
		fields := make([]apierror.FieldError, 0, len(validation.Issues))
		for _, issue := range validation.Issues {
			fields = append(fields, apierror.FieldError{Field: issue.Field, Code: issue.Code})
		}
		apierror.Respond(c, apierror.Validation(fields...))
	case errors.Is(err, domain.ErrResourceNotFound):
		apierror.Respond(c, apierror.ErrNotFound)
	case errors.Is(err, domain.ErrUserNotFound):
		// The token was valid but the user vanished between the check and the lookup.
		apierror.Respond(c, apierror.ErrUnauthorized)
	default:
		apierror.Respond(c, err)
	}
}
