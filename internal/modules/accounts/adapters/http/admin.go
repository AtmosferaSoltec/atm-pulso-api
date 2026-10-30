package http

import (
	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/logger"
)

// AdminRoutes mounts the platform administrator's routes (behind auth.Guard.Superadmin).
// console-web calls them: clients and gyms come from atm-auth-api, branches are created here
// because Atmósfera charges for each one.
func (h *Handler) AdminRoutes(admin gin.IRoutes) {
	admin.GET("/clients", h.listClients)
	admin.PATCH("/gyms/:gym_id", h.updateGymIdentity)
	admin.POST("/gyms/:gym_id/branches", h.addBranch)
	admin.PATCH("/branches/:id", h.updateBranch)
	admin.DELETE("/branches/:id", h.deleteBranch)
}

// audit records who changed what. Only ids: no names, documents or addresses.
func audit(c *gin.Context, action string, attrs ...any) {
	claims, _ := auth.ClaimsFromContext(c.Request.Context())
	logger.From(c.Request.Context()).Info("admin action", append([]any{"action", action, "admin_id", claims.Subject}, attrs...)...)
}

func (h *Handler) listClients(c *gin.Context) {
	clients, err := h.service.ListClients(c.Request.Context())
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, clients)
}

func (h *Handler) updateGymIdentity(c *gin.Context) {
	gymID, ok := pathID(c, "gym_id")
	if !ok {
		return
	}
	var input app.GymIdentity
	if !bind(c, &input) {
		return
	}
	gym, err := h.service.UpdateGymIdentity(c.Request.Context(), gymID, input)
	if err != nil {
		respond(c, err)
		return
	}
	audit(c, "gym.identity", "gym_id", gymID)
	apierror.OK(c, gym)
}

type branchRequest struct {
	Name    string  `json:"name"`
	Address *string `json:"address"`
}

func (h *Handler) addBranch(c *gin.Context) {
	gymID, ok := pathID(c, "gym_id")
	if !ok {
		return
	}
	var req branchRequest
	if !bind(c, &req) {
		return
	}
	branch, err := h.service.AddBranch(c.Request.Context(), gymID, req.Name, req.Address)
	if err != nil {
		respond(c, err)
		return
	}
	audit(c, "branch.create", "gym_id", gymID, "branch_id", branch.ID)
	apierror.Created(c, branch)
}

func (h *Handler) updateBranch(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	var changes app.BranchChanges
	if !bind(c, &changes) {
		return
	}
	branch, err := h.service.UpdateBranch(c.Request.Context(), id, changes)
	if err != nil {
		respond(c, err)
		return
	}
	audit(c, "branch.update", "branch_id", id)
	apierror.OK(c, branch)
}

func (h *Handler) deleteBranch(c *gin.Context) {
	id, ok := pathID(c, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteBranch(c.Request.Context(), id); err != nil {
		respond(c, err)
		return
	}
	audit(c, "branch.delete", "branch_id", id)
	apierror.NoContent(c)
}
