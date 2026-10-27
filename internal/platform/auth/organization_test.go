package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type existingOrganization struct{}

func (existingOrganization) OrganizationExists(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func TestInspectionCannotWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Request = c.Request.WithContext(WithPrincipal(c.Request.Context(), Principal{Role: RoleSuperadmin}))
			c.Next()
		})
		router.Handle(method, "/private", OrganizationScope(existingOrganization{}), func(c *gin.Context) { t.Error("write handler reached"); c.Status(204) })
		request := httptest.NewRequest(method, "/private", nil)
		request.Header.Set("X-Organization-Id", uuid.NewString())
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		assert.Equal(t, http.StatusForbidden, recorder.Code)
	}
}
