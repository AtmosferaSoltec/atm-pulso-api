// Package http exposes the body_tracking use cases over Gin. Every route sits behind the module guard.
package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// Handler serves /members/{id}/measurements, /members/{id}/routines and /members/{id}/body-access-log.
type Handler struct{ service *app.Service }

// NewHandler builds the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// Routes mounts the endpoints; each one requires the body_tracking module of the gym.
func (h *Handler) Routes(protected gin.IRoutes) {
	on := auth.RequireFeature(auth.FeatureBodyTracking)
	protected.GET("/members/:id/measurements", on, h.measurements)
	protected.POST("/members/:id/measurements", on, h.createMeasurement)
	protected.PUT("/members/:id/measurements/:record_id", on, h.updateMeasurement)
	protected.DELETE("/members/:id/measurements/:record_id", on, h.deleteMeasurement)
	protected.GET("/members/:id/routines", on, h.routines)
	protected.POST("/members/:id/routines", on, h.createRoutine)
	protected.PUT("/members/:id/routines/:record_id", on, h.updateRoutine)
	protected.DELETE("/members/:id/routines/:record_id", on, h.deleteRoutine)
	protected.GET("/members/:id/body-access-log", on, h.accessLog)
}

func principal(c *gin.Context) auth.Principal {
	p, _ := auth.FromContext(c.Request.Context())
	return p
}

func (h *Handler) measurements(c *gin.Context) {
	member, ok := idParam(c, "id")
	if !ok {
		return
	}
	list, err := h.service.Measurements(c.Request.Context(), principal(c), member)
	reply(c, list, err)
}

func (h *Handler) createMeasurement(c *gin.Context) {
	member, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in app.MeasurementInput
	if !bind(c, &in) {
		return
	}
	view, existed, err := h.service.CreateMeasurement(c.Request.Context(), principal(c), member, in)
	created(c, view, existed, err)
}

func (h *Handler) updateMeasurement(c *gin.Context) {
	member, record, ok := twoIDs(c)
	if !ok {
		return
	}
	var in app.MeasurementInput
	if !bind(c, &in) {
		return
	}
	view, err := h.service.UpdateMeasurement(c.Request.Context(), principal(c), member, record, in)
	reply(c, view, err)
}

func (h *Handler) deleteMeasurement(c *gin.Context) {
	member, record, ok := twoIDs(c)
	if !ok {
		return
	}
	gone(c, h.service.DeleteMeasurement(c.Request.Context(), principal(c), member, record))
}

func (h *Handler) routines(c *gin.Context) {
	member, ok := idParam(c, "id")
	if !ok {
		return
	}
	list, err := h.service.Routines(c.Request.Context(), principal(c), member)
	reply(c, list, err)
}

func (h *Handler) createRoutine(c *gin.Context) {
	member, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in app.RoutineInput
	if !bind(c, &in) {
		return
	}
	view, existed, err := h.service.CreateRoutine(c.Request.Context(), principal(c), member, in)
	created(c, view, existed, err)
}

func (h *Handler) updateRoutine(c *gin.Context) {
	member, record, ok := twoIDs(c)
	if !ok {
		return
	}
	var in app.RoutineInput
	if !bind(c, &in) {
		return
	}
	view, err := h.service.UpdateRoutine(c.Request.Context(), principal(c), member, record, in)
	reply(c, view, err)
}

func (h *Handler) deleteRoutine(c *gin.Context) {
	member, record, ok := twoIDs(c)
	if !ok {
		return
	}
	gone(c, h.service.DeleteRoutine(c.Request.Context(), principal(c), member, record))
}

func (h *Handler) accessLog(c *gin.Context) {
	member, ok := idParam(c, "id")
	if !ok {
		return
	}
	list, err := h.service.AccessLog(c.Request.Context(), principal(c), member)
	reply(c, list, err)
}

func reply(c *gin.Context, data any, err error) {
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, data)
}

func created(c *gin.Context, data any, existed bool, err error) {
	switch {
	case err != nil:
		respond(c, err)
	case existed:
		apierror.OK(c, data)
	default:
		apierror.Created(c, data)
	}
}

func gone(c *gin.Context, err error) {
	if err != nil {
		respond(c, err)
		return
	}
	apierror.NoContent(c)
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

func idParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil || id == uuid.Nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func twoIDs(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	member, ok := idParam(c, "id")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	record, ok := idParam(c, "record_id")
	return member, record, ok
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
		apierror.Respond(c, apierror.New(http.StatusNotFound, "MEMBER_NOT_FOUND", "El miembro ya no existe."))
	case errors.Is(err, domain.ErrMeasurementNotFound):
		apierror.Respond(c, apierror.New(http.StatusNotFound, "MEASUREMENT_NOT_FOUND", "La medición ya no existe."))
	case errors.Is(err, domain.ErrRoutineNotFound):
		apierror.Respond(c, apierror.New(http.StatusNotFound, "ROUTINE_NOT_FOUND", "La rutina ya no existe."))
	case errors.Is(err, domain.ErrBranchNotAccessible):
		apierror.Respond(c, apierror.New(http.StatusForbidden, "BRANCH_NOT_ACCESSIBLE", "No tienes acceso a la sede de este miembro."))
	case errors.Is(err, domain.ErrForbidden):
		apierror.Respond(c, apierror.ErrForbidden)
	case errors.Is(err, domain.ErrConflict):
		apierror.Respond(c, apierror.New(http.StatusConflict, "CONFLICT", "El registro ya existe."))
	default:
		apierror.Respond(c, err)
	}
}
