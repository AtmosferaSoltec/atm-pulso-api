package http

import (
	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

func (h *Handler) listUsers(c *gin.Context) {
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	users, total, err := h.service.ListUsers(c.Request.Context(), p, page, size)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Page(c, users, apierror.Meta{Page: page, PageSize: size, Total: total})
}

func (h *Handler) createUser(c *gin.Context) {
	var input app.NewUser
	if !bind(c, &input) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	user, err := h.service.CreateUser(c.Request.Context(), p, input)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Created(c, user)
}

func (h *Handler) updateUser(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var input app.UserChanges
	if !bind(c, &input) {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	user, err := h.service.UpdateUser(c.Request.Context(), p, id, input)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, user)
}

func (h *Handler) activateUser(c *gin.Context)   { h.setUserActive(c, true) }
func (h *Handler) deactivateUser(c *gin.Context) { h.setUserActive(c, false) }

func (h *Handler) setUserActive(c *gin.Context, active bool) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	user, err := h.service.SetUserActive(c.Request.Context(), p, id, active)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, user)
}

func (h *Handler) resetUserPassword(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	p, _ := auth.FromContext(c.Request.Context())
	user, err := h.service.ResetUserPassword(c.Request.Context(), p, id)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, user)
}
