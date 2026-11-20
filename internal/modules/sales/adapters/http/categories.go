package http

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/logger"
)

// AdminRoutes mounts the platform administrator's management of a gym's product categories (behind
// auth.Guard.Superadmin); console-web loads them on the gym's behalf.
func (h *Handler) AdminRoutes(admin gin.IRoutes) {
	admin.GET("/gyms/:gym_id/product-categories", h.adminCategories)
	admin.POST("/gyms/:gym_id/product-categories", h.adminCreateCategories)
	admin.PATCH("/gyms/:gym_id/product-categories/:id", h.adminUpdateCategory)
	admin.DELETE("/gyms/:gym_id/product-categories/:id", h.adminDeleteCategory)
}

func (h *Handler) categories(c *gin.Context) {
	list, err := h.service.Categories(c.Request.Context(), principal(c))
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, list)
}

func (h *Handler) createCategory(c *gin.Context) {
	var in app.CategoryInput
	if !bind(c, &in) {
		return
	}
	category, err := h.service.CreateCategory(c.Request.Context(), principal(c), in)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.Created(c, category)
}

func (h *Handler) updateCategory(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in app.CategoryChanges
	if !bind(c, &in) {
		return
	}
	category, err := h.service.UpdateCategory(c.Request.Context(), principal(c), id, in)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, category)
}

func (h *Handler) deleteCategory(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteCategory(c.Request.Context(), principal(c), id); err != nil {
		respond(c, err)
		return
	}
	apierror.NoContent(c)
}

func gymID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("gym_id"))
	if err != nil || id == uuid.Nil {
		apierror.Respond(c, apierror.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

// audit records who changed what. Only ids: no names.
func audit(c *gin.Context, action string, attrs ...any) {
	claims, _ := auth.ClaimsFromContext(c.Request.Context())
	logger.From(c.Request.Context()).Info("admin action", append([]any{"action", action, "admin_id", claims.Subject}, attrs...)...)
}

func (h *Handler) adminCategories(c *gin.Context) {
	gym, ok := gymID(c)
	if !ok {
		return
	}
	list, err := h.service.AdminCategories(c.Request.Context(), gym)
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, list)
}

func (h *Handler) adminCreateCategories(c *gin.Context) {
	gym, ok := gymID(c)
	if !ok {
		return
	}
	var in app.BulkCategoriesInput
	if !bind(c, &in) {
		return
	}
	result, err := h.service.AdminCreateCategories(c.Request.Context(), gym, in)
	if err != nil {
		respond(c, err)
		return
	}
	audit(c, "product_categories.create", "gym_id", gym, "created", len(result.Created))
	// 201 when something was created; 200 when every name was skipped.
	if len(result.Created) > 0 {
		apierror.Created(c, result)
		return
	}
	apierror.OK(c, result)
}

func (h *Handler) adminUpdateCategory(c *gin.Context) {
	gym, ok := gymID(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in app.CategoryChanges
	if !bind(c, &in) {
		return
	}
	category, err := h.service.AdminUpdateCategory(c.Request.Context(), gym, id, in)
	if err != nil {
		respond(c, err)
		return
	}
	audit(c, "product_categories.update", "gym_id", gym, "category_id", id)
	apierror.OK(c, category)
}

func (h *Handler) adminDeleteCategory(c *gin.Context) {
	gym, ok := gymID(c)
	if !ok {
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.service.AdminDeleteCategory(c.Request.Context(), gym, id); err != nil {
		respond(c, err)
		return
	}
	audit(c, "product_categories.delete", "gym_id", gym, "category_id", id)
	apierror.NoContent(c)
}
