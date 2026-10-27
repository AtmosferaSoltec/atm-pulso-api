package http

import (
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
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
