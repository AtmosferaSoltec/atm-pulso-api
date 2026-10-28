package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/httpserver"
)

// memoryLogos is the object store in memory: tests never reach Cloudflare.
type memoryLogos struct {
	mu      sync.Mutex
	objects map[string]stored
}

type stored struct {
	contentType string
	data        []byte
}

func newMemoryLogos() *memoryLogos { return &memoryLogos{objects: map[string]stored{}} }

func (m *memoryLogos) Put(_ context.Context, key, contentType string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = stored{contentType, data}
	return nil
}

func (m *memoryLogos) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memoryLogos) Get(_ context.Context, key string) (app.LogoFile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	object, ok := m.objects[key]
	if !ok {
		return app.LogoFile{}, domain.ErrResourceNotFound
	}
	return app.LogoFile{Body: io.NopCloser(bytes.NewReader(object.data)), ContentType: object.contentType, Size: int64(len(object.data))}, nil
}

var (
	pngImage  = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	jpegImage = append([]byte("\xff\xd8\xff\xe0"), make([]byte, 64)...)
)

// putLogo sends the image as the raw body, like the web does.
func (h harness) putLogo(t *testing.T, token, gym string, body []byte) response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, httpserver.APIPrefix+"/gyms/"+gym+"/logo", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	out := response{status: rec.Code, header: rec.Header()}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

func TestGymLogo(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	owner, admin := h.token(t, tn.Owner, demoPassword), h.token(t, tn.Admin, demoPassword)
	super := h.token(t, h.newSuperadmin(t), demoPassword)
	gym := tn.GymOne.String()

	// Only the superadmin sets the logo.
	for _, token := range []string{owner, admin, h.token(t, tn.Receptionist, demoPassword)} {
		assert.Equal(t, 403, h.putLogo(t, token, gym, pngImage).status)
		assert.Equal(t, 403, h.call(t, http.MethodDelete, "/gyms/"+gym+"/logo", token, nil).status)
	}
	assert.Equal(t, 404, h.putLogo(t, super, "00000000-0000-7000-8000-000000000000", pngImage).status)

	// Only real PNG, JPEG and WebP images of a sensible size are accepted.
	for name, body := range map[string][]byte{
		"empty":    {},
		"text":     []byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>"),
		"declared": []byte("GIF89a" + strings.Repeat("x", 64)),
		"huge":     append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600<<10)...),
	} {
		res := h.putLogo(t, super, gym, body)
		assert.Equal(t, 400, res.status, name)
	}
	assert.Empty(t, h.logos.objects, "a rejected image is never stored")

	first := h.putLogo(t, super, gym, pngImage)
	require.Equal(t, 200, first.status, first.body)
	url, _ := first.data()["logo_url"].(string)
	require.True(t, strings.HasPrefix(url, "/gyms/"+gym+"/logo?v="), url)
	assert.Len(t, h.logos.objects, 1)

	// Anyone can load it (an <img> sends no token), straight from the API and cacheable for good.
	served := h.callRaw(t, http.MethodGet, url, "")
	require.Equal(t, 200, served.Code)
	assert.Equal(t, pngImage, served.Body.Bytes())
	assert.Equal(t, "image/png", served.Header().Get("Content-Type"))
	assert.Contains(t, served.Header().Get("Cache-Control"), "immutable")
	stale := h.callRaw(t, http.MethodGet, "/gyms/"+gym+"/logo?v=old", "")
	assert.Equal(t, 200, stale.Code)
	assert.Equal(t, "no-cache", stale.Header().Get("Cache-Control"))
	assert.Equal(t, 404, h.callRaw(t, http.MethodGet, "/gyms/"+other.GymOne.String()+"/logo", "").Code, "a gym without logo")

	// The catalog of the client carries it.
	catalog := h.call(t, http.MethodGet, "/catalog", owner, nil)
	require.Equal(t, 200, catalog.status)
	gyms, _ := catalog.data()["gyms"].([]any)
	var seen bool
	for _, item := range gyms {
		if g := item.(map[string]any); g["id"] == gym {
			assert.Equal(t, url, g["logo_url"])
			seen = true
		}
	}
	assert.True(t, seen)

	// Replacing it deletes the previous file.
	second := h.putLogo(t, super, gym, jpegImage)
	require.Equal(t, 200, second.status, second.body)
	assert.NotEqual(t, url, second.data()["logo_url"])
	assert.Len(t, h.logos.objects, 1, "the old logo is removed from the store")

	// Another client's gym is untouched, and removing clears everything.
	assert.Nil(t, h.call(t, http.MethodGet, "/catalog", h.token(t, other.Owner, demoPassword), nil).data()["gyms"].([]any)[0].(map[string]any)["logo_url"])
	removed := h.call(t, http.MethodDelete, "/gyms/"+gym+"/logo", super, nil)
	require.Equal(t, 200, removed.status, removed.body)
	assert.Nil(t, removed.data()["logo_url"])
	assert.Empty(t, h.logos.objects)
}
