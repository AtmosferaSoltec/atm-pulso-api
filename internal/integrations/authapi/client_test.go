package authapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPClient(t *testing.T) {
	var tokenCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["client_id"] != "pulso" || body["client_secret"] != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "svc", "expires_in": 600})
	})
	guard := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer svc" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /v1/service/tenants", guard(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []Tenant{{ID: "t1", RUC: "20123456789", LegalName: "ATLAS SAC", DisplayName: "Atlas Gym"}}})
	}))
	mux.HandleFunc("POST /v1/service/members", guard(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "t1", r.URL.Query().Get("tenant_id"))
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"protected_member"}}`))
	}))
	mux.HandleFunc("GET /v1/service/members", guard(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewHTTPClient(server.URL, "pulso", "s3cret", 2*time.Second)
	ctx := context.Background()

	tenant, err := client.Tenant(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, "Atlas Gym", tenant.Name())
	_, err = client.Tenant(ctx, "other")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.EqualValues(t, 1, tokenCalls.Load(), "the service token is cached")

	_, err = client.AddMember(ctx, "t1", NewMember{DNI: "12345678", FullName: "Ana", Role: "admin"})
	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "protected_member", apiErr.Code)

	_, err = client.Members(ctx, "t1")
	assert.ErrorIs(t, err, ErrUnavailable)

	bad := NewHTTPClient(server.URL, "pulso", "wrong", 2*time.Second)
	_, err = bad.Tenant(ctx, "t1")
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.Status)
}
