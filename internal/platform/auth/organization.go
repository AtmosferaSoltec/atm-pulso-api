package auth

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
)

// Organizations validates the only request-supplied inspection scope.
type Organizations interface {
	OrganizationExists(context.Context, uuid.UUID) (bool, error)
}

// OrganizationScope allows superadmin GET inspection and preserves every other user's live scope.
func OrganizationScope(organizations Organizations) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, ok := FromContext(c.Request.Context())
		if !ok {
			apierror.Respond(c, apierror.ErrUnauthorized)
			return
		}
		if principal.Role != RoleSuperadmin {
			c.Next()
			return
		}
		if c.Request.Method != http.MethodGet {
			apierror.Respond(c, apierror.ErrForbidden)
			return
		}
		id, err := uuid.Parse(c.GetHeader("X-Organization-Id"))
		if err != nil || id == uuid.Nil {
			apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "X-Organization-Id", Code: "ORGANIZATION_REQUIRED"}))
			return
		}
		exists, err := organizations.OrganizationExists(c.Request.Context(), id)
		if err != nil {
			apierror.Respond(c, err)
			return
		}
		if !exists {
			apierror.Respond(c, apierror.ErrNotFound)
			return
		}
		principal.OrganizationID = id
		c.Request = c.Request.WithContext(WithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
}
