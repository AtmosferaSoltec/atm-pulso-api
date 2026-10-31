package storageapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tokens struct{ forgotten int }

func (tokens) ServiceToken(context.Context) (string, error) { return "service-token", nil }
func (t *tokens) ForgetToken()                              { t.forgotten++ }

func TestPreviewLinksAsksWithTheServiceToken(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/files/preview", r.URL.Path)
		assert.Equal(t, "Bearer service-token", r.Header.Get("Authorization"))
		var body struct{ IDs []string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, []string{"f1"}, body.IDs)
		_ = json.NewEncoder(w).Encode(map[string]any{"items": map[string]any{"f1": map[string]any{"url": "https://signed", "expires_at": expires}}})
	}))
	defer server.Close()

	links, err := NewHTTPClient(server.URL, &tokens{}, time.Second).PreviewLinks(t.Context(), []string{"f1"})
	require.NoError(t, err)
	assert.Equal(t, "https://signed", links["f1"].URL)
	assert.True(t, links["f1"].ExpiresAt.Equal(expires))
}

func TestPreviewLinksFailuresAreUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		source := &tokens{}
		_, err := NewHTTPClient(server.URL, source, time.Second).PreviewLinks(t.Context(), []string{"f1"})
		assert.ErrorIs(t, err, ErrUnavailable, status)
		if status == http.StatusUnauthorized {
			assert.Equal(t, 1, source.forgotten, "a refused token is dropped")
		}
		server.Close()
	}
	_, err := NewHTTPClient("http://127.0.0.1:1", &tokens{}, time.Second).PreviewLinks(t.Context(), []string{"f1"})
	assert.ErrorIs(t, err, ErrUnavailable)
}
