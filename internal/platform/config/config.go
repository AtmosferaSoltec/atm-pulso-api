// Package config reads and validates the environment variables at startup. If a
// required one is missing or malformed the app does not start: an incomplete .env
// shows up during the deploy, not while serving a person.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Environment is the value of APP_ENV.
type Environment string

// Supported environments.
const (
	Development Environment = "development"
	Production  Environment = "production"
	Test        Environment = "test"
)

const (
	defaultIdentityURL   = "http://identity-api:8080"
	defaultStorageAPIURL = "http://storage-api:8084"
	defaultAuthURL       = "http://auth-api:8080"
	defaultSystemCode    = "pulso"
)

// Config is the validated environment.
type Config struct {
	Env                     Environment
	Port                    string
	DatabaseURL             string
	CORSOrigins             []string
	RateLimitPerMinute      int
	LoginRateLimitPerMinute int
	Auth                    AuthConfig
	Identity                IdentityConfig
	StorageAPI              StorageAPIConfig
	Storage                 StorageConfig
}

// AuthConfig is how to trust and call atm-auth-api, which owns identity and access.
type AuthConfig struct {
	// URL is where atm-auth-api answers; its public keys are at URL + /.well-known/jwks.json.
	URL string
	// Issuer is the `iss` of its tokens. Required in production.
	Issuer string
	// SystemCode is Pulso's code in atm-auth-api.
	SystemCode string
	// ClientSecret is the secret of Pulso's service token. Without it (development only)
	// company names and team management are unavailable.
	ClientSecret string
}

// JWKSURL is where the public keys of atm-auth-api are published.
func (a AuthConfig) JWKSURL() string { return a.URL + "/.well-known/jwks.json" }

// IdentityConfig is how to reach atm-identity-api, which resolves DNI and RUC.
// Without an API key (development only) a deterministic fake answers instead.
type IdentityConfig struct {
	URL    string
	APIKey string
}

// StorageAPIConfig is how to reach atm-storage-api, which holds the company logos. Pulso reads them with
// its service token (AUTH_CLIENT_SECRET); without that secret no logo is resolved.
type StorageAPIConfig struct{ URL string }

// StorageConfig is the Cloudflare R2 bucket that holds the payment vouchers. Without it
// (development only) the voucher endpoints answer that storage is unavailable.
type StorageConfig struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

// Enabled says whether every storage variable is set.
func (s StorageConfig) Enabled() bool {
	return s.Endpoint != "" && s.Bucket != "" && s.AccessKeyID != "" && s.SecretAccessKey != ""
}

// IsProduction says whether the app runs on the server.
func (c Config) IsProduction() bool { return c.Env == Production }

// Load reads the process environment.
func Load() (Config, error) {
	return LoadFrom(os.Getenv)
}

// LoadFrom reads the variables with lookup, so it can be tested without touching the environment.
func LoadFrom(lookup func(string) string) (Config, error) {
	r := reader{lookup: lookup}

	cfg := Config{
		Env:                     r.environment("APP_ENV"),
		Port:                    r.port("PORT"),
		DatabaseURL:             r.databaseURL("DATABASE_URL"),
		CORSOrigins:             r.list("CORS_ORIGIN"),
		RateLimitPerMinute:      r.positiveIntOr("RATE_LIMIT_PER_MINUTE", 120),
		LoginRateLimitPerMinute: r.positiveIntOr("LOGIN_RATE_LIMIT_PER_MINUTE", 10),
		Auth: AuthConfig{
			URL:          strings.TrimRight(r.optionalOr("AUTH_API_URL", defaultAuthURL), "/"),
			Issuer:       r.optional("AUTH_ISSUER"),
			SystemCode:   r.optionalOr("AUTH_SYSTEM_CODE", defaultSystemCode),
			ClientSecret: r.optional("AUTH_CLIENT_SECRET"),
		},
		Identity: IdentityConfig{
			URL:    r.optionalOr("IDENTITY_API_URL", defaultIdentityURL),
			APIKey: r.optional("IDENTITY_API_KEY"),
		},
		StorageAPI: StorageAPIConfig{URL: r.httpURL("STORAGE_API_URL")},
		Storage: StorageConfig{
			Endpoint:        r.httpURL("R2_ENDPOINT"),
			Bucket:          r.optional("R2_BUCKET"),
			AccessKeyID:     r.optional("R2_ACCESS_KEY_ID"),
			SecretAccessKey: r.optional("R2_SECRET_ACCESS_KEY"),
		},
	}

	if cfg.StorageAPI.URL == "" {
		cfg.StorageAPI.URL = defaultStorageAPIURL
	}

	if cfg.IsProduction() {
		if strings.HasPrefix(cfg.Storage.Endpoint, "http://") {
			r.fail("R2_ENDPOINT must be an https:// URL in production")
		}
		r.requireInProduction("AUTH_ISSUER", cfg.Auth.Issuer != "")
		r.requireInProduction("AUTH_CLIENT_SECRET", cfg.Auth.ClientSecret != "")
		r.requireInProduction("IDENTITY_API_KEY", cfg.Identity.APIKey != "")
		r.requireInProduction("R2_ENDPOINT, R2_BUCKET, R2_ACCESS_KEY_ID, and R2_SECRET_ACCESS_KEY", cfg.Storage.Enabled())
	}

	if len(r.problems) > 0 {
		return Config{}, fmt.Errorf("invalid environment:\n  - %s", strings.Join(r.problems, "\n  - "))
	}
	return cfg, nil
}

// DatabaseURL is the only thing the migrate subcommand needs.
func DatabaseURL() (string, error) {
	r := reader{lookup: os.Getenv}
	value := r.databaseURL("DATABASE_URL")
	if len(r.problems) > 0 {
		return "", errors.New(r.problems[0])
	}
	return value, nil
}

type reader struct {
	lookup   func(string) string
	problems []string
}

func (r *reader) fail(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

func (r *reader) required(name string) string {
	value := strings.TrimSpace(r.lookup(name))
	if value == "" {
		r.fail("%s is required", name)
	}
	return value
}

func (r *reader) optional(name string) string {
	return strings.TrimSpace(r.lookup(name))
}

func (r *reader) optionalOr(name, fallback string) string {
	if value := r.optional(name); value != "" {
		return value
	}
	return fallback
}

func (r *reader) environment(name string) Environment {
	value := Environment(r.required(name))
	switch value {
	case Development, Production, Test, "":
	default:
		r.fail("%s must be development, production or test (got %q)", name, value)
	}
	return value
}

func (r *reader) port(name string) string {
	value := r.required(name)
	if value == "" {
		return value
	}
	if n, err := strconv.Atoi(value); err != nil || n < 1 || n > 65535 {
		r.fail("%s must be a port number (got %q)", name, value)
	}
	return value
}

func (r *reader) databaseURL(name string) string {
	value := r.required(name)
	if value == "" {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		// The value is never printed: it carries the password.
		r.fail("%s must be a postgresql:// URL", name)
	}
	return value
}

// httpURL is optional, but when present it must be an absolute http(s) URL (a trailing slash is
// dropped). Plain http is only for a local S3-compatible server; production demands https.
// The value is not printed: it may carry an account identifier.
func (r *reader) httpURL(name string) string {
	value := strings.TrimRight(r.optional(name), "/")
	if value == "" {
		return value
	}
	if parsed, err := url.Parse(value); err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		r.fail("%s must be an http(s):// URL", name)
	}
	return value
}

func (r *reader) positiveIntOr(name string, fallback int) int {
	value := r.optional(name)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		r.fail("%s must be a positive whole number (got %q)", name, value)
		return fallback
	}
	return n
}

func (r *reader) list(name string) []string {
	var items []string
	for _, item := range strings.Split(r.optional(name), ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func (r *reader) requireInProduction(name string, present bool) {
	if !present {
		r.fail("%s is required in production", name)
	}
}
