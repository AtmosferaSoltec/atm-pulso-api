package http

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

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

func (h *Handler) listClients(c *gin.Context) {
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	principal, _ := auth.FromContext(c.Request.Context())
	clients, total, err := h.service.ListClients(c.Request.Context(), principal, c.Query("search"), page, size)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Page(c, clients, apierror.Meta{Page: page, PageSize: size, Total: total})
}
func (h *Handler) clientSummary(c *gin.Context) {
	principal, _ := auth.FromContext(c.Request.Context())
	summary, err := h.service.ClientSummary(c.Request.Context(), principal)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, summary)
}
func (h *Handler) createClient(c *gin.Context) {
	var input app.NewClient
	if !bind(c, &input) {
		return
	}
	principal, _ := auth.FromContext(c.Request.Context())
	client, err := h.service.CreateClient(c.Request.Context(), principal, input)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Created(c, client)
}
func (h *Handler) addGym(c *gin.Context) {
	id, ok := pathID(c, "client_id")
	if !ok {
		return
	}
	var input app.NewGym
	if !bind(c, &input) {
		return
	}
	principal, _ := auth.FromContext(c.Request.Context())
	gym, err := h.service.AddGym(c.Request.Context(), principal, id, input)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Created(c, gym)
}
func (h *Handler) addBranch(c *gin.Context) {
	id, ok := pathID(c, "gym_id")
	if !ok {
		return
	}
	var input app.NewBranch
	if !bind(c, &input) {
		return
	}
	principal, _ := auth.FromContext(c.Request.Context())
	branch, err := h.service.AddBranch(c.Request.Context(), principal, id, input)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Created(c, branch)
}
