package http

import (
	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

func (h *Handler) plans(c *gin.Context) {
	p, _ := auth.FromContext(c.Request.Context())
	plans, err := h.service.Plans(c.Request.Context(), p)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, plans)
}

func (h *Handler) createPlan(c *gin.Context) {
	var in app.PlanInput
	if !bind(c, &in) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	plan, err := h.service.CreatePlan(c.Request.Context(), p, in)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Created(c, plan)
}

func (h *Handler) updatePlan(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var changes app.PlanChanges
	if !bind(c, &changes) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	plan, err := h.service.UpdatePlan(c.Request.Context(), p, id, changes)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, plan)
}
