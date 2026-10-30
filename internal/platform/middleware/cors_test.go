package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/middleware"
)

func TestPreflightAllowsTheHeadersTheWebSends(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORS([]string{"https://pulso.example"}))
	router.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodOptions, "/x", nil)
	req.Header.Set("Origin", "https://pulso.example")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	// The browser refuses a request whose headers the preflight did not allow: the token,
	// the JSON body and the superadmin's inspection header must all be there.
	for _, header := range []string{"Authorization", "Content-Type"} {
		assert.Contains(t, allowed, header)
	}
	assert.Equal(t, "https://pulso.example", rec.Header().Get("Access-Control-Allow-Origin"))

	other := httptest.NewRequest(http.MethodOptions, "/x", nil)
	other.Header.Set("Origin", "https://evil.example")
	otherRec := httptest.NewRecorder()
	router.ServeHTTP(otherRec, other)
	assert.Empty(t, otherRec.Header().Get("Access-Control-Allow-Origin"), "an origin outside the list gets no CORS headers")
}
