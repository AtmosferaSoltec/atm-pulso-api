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
	"time"
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
	// minSecretLength: `openssl rand -base64 48` produces 64 characters.
	minSecretLength = 32
	// defaultAccessTokenTTL is one work shift. The token only lives in the web's
	// memory and there is no refresh token, so this is also how long a login lasts.
	defaultAccessTokenTTL = 8 * time.Hour
	maxAccessTokenTTL     = 24 * time.Hour
	defaultIdentityURL    = "http://identity-api:8080"
)

// Config is the validated environment.
type Config struct {
	Env                     Environment
	Port                    string
	DatabaseURL             string
	JWTAccessSecret         string
	AccessTokenTTL          time.Duration
	CORSOrigins             []string
	RateLimitPerMinute      int
	LoginRateLimitPerMinute int
	Identity                IdentityConfig
}

// IdentityConfig is how to reach atm-identity-api, which resolves DNI and RUC.
// Without an API key (development only) a deterministic fake answers instead.
type IdentityConfig struct {
	URL    string
	APIKey string
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
		JWTAccessSecret:         r.secret("JWT_ACCESS_SECRET"),
		AccessTokenTTL:          r.accessTokenTTL("ACCESS_TOKEN_TTL_SECONDS"),
		CORSOrigins:             r.list("CORS_ORIGIN"),
		RateLimitPerMinute:      r.positiveIntOr("RATE_LIMIT_PER_MINUTE", 120),
		LoginRateLimitPerMinute: r.positiveIntOr("LOGIN_RATE_LIMIT_PER_MINUTE", 10),
		Identity: IdentityConfig{
			URL:    r.optionalOr("IDENTITY_API_URL", defaultIdentityURL),
			APIKey: r.optional("IDENTITY_API_KEY"),
		},
	}

	if cfg.IsProduction() {
		r.requireInProduction("IDENTITY_API_KEY", cfg.Identity.APIKey != "")
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

func (r *reader) secret(name string) string {
	value := r.required(name)
	if value != "" && len(value) < minSecretLength {
		r.fail("%s must have at least %d characters (generate it with: openssl rand -base64 48)", name, minSecretLength)
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

func (r *reader) accessTokenTTL(name string) time.Duration {
	value := r.optional(name)
	if value == "" {
		return defaultAccessTokenTTL
	}
	seconds, err := strconv.Atoi(value)
	ttl := time.Duration(seconds) * time.Second
	if err != nil || ttl <= 0 || ttl > maxAccessTokenTTL {
		r.fail("%s must be between 1 and %d seconds (got %q)", name, int(maxAccessTokenTTL.Seconds()), value)
		return defaultAccessTokenTTL
	}
	return ttl
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
