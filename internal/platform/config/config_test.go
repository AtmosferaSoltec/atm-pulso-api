package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

var development = map[string]string{
	"APP_ENV": "development", "PORT": "3001",
	"DATABASE_URL": "postgres://pulso_app:x@127.0.0.1:5432/pulso",
}

func with(extra map[string]string) map[string]string {
	merged := map[string]string{}
	for k, v := range development {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}

func TestAuthDefaultsInDevelopment(t *testing.T) {
	cfg, err := LoadFrom(env(with(map[string]string{"AUTH_API_URL": "http://localhost:3130/"})))
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:3130", cfg.Auth.URL, "a trailing slash is dropped")
	assert.Equal(t, "http://localhost:3130/.well-known/jwks.json", cfg.Auth.JWKSURL())
	assert.Equal(t, "pulso", cfg.Auth.SystemCode)
	assert.Empty(t, cfg.Auth.ClientSecret, "the service secret is optional in development")

	cfg, err = LoadFrom(env(development))
	require.NoError(t, err)
	assert.Equal(t, "http://auth-api:8080", cfg.Auth.URL)
}

func TestProductionNeedsTheAuthIssuerAndTheServiceSecret(t *testing.T) {
	_, err := LoadFrom(env(with(map[string]string{"APP_ENV": "production"})))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AUTH_ISSUER is required in production")
	assert.Contains(t, err.Error(), "AUTH_CLIENT_SECRET is required in production")
	assert.NotContains(t, err.Error(), "JWT_ACCESS_SECRET", "passwords and tokens are atm-auth-api's now")
}
