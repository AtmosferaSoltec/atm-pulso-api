// Package middleware has what runs on every request: request id, panic recovery,
// access log, CORS, per-IP limit and body size.
package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/logger"
)

// RequestIDHeader travels in the request and in the response.
const RequestIDHeader = "X-Request-Id"

// A client's request id is only honored if it cannot pollute the logs.
var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// RequestID takes the client's X-Request-Id if it is safe, or generates one, and
// leaves in the context a logger that carries it on every line.
func RequestID(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !safeRequestID.MatchString(id) {
			id = ids.New().String()
		}
		c.Header(RequestIDHeader, id)
		ctx := logger.WithContext(c.Request.Context(), base.With("request_id", id))
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// Recover turns a panic into a 500 with the error format. A broken handler neither
// takes the process down nor leaves the other requests unanswered.
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.From(c.Request.Context()).Error("panic recovered",
					"panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
				apierror.Respond(c, apierror.ErrInternal)
			}
		}()
		c.Next()
	}
}

// AccessLog records method, route, status and duration. Never headers or bodies:
// they carry tokens, passwords and personal data. The health check is not logged:
// Docker calls it every 30 seconds.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		path := c.FullPath()
		if strings.HasSuffix(path, "/health") || strings.HasSuffix(path, "/health/ready") {
			return
		}
		if path == "" {
			path = "unmatched"
		}
		logger.From(c.Request.Context()).Info("request",
			"method", c.Request.Method,
			"route", path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

// CORS lets through only the configured origins. In production the web and the API
// share one origin (nginx proxies /api), so this only matters for local development.
// The token travels in the Authorization header, never in a cookie.
func CORS(allowed []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && slices.Contains(allowed, origin) {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", RequestIDHeader+", Retry-After")
			if c.Request.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				// X-Organization-Id is how the superadmin inspects one client (GET only).
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Organization-Id, "+RequestIDHeader)
				h.Set("Access-Control-Max-Age", "600")
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Next()
	}
}

// SecurityHeaders sets the headers of a JSON API: no sniffing, no framing, no referrer,
// and nothing cached (responses carry personal data and depend on the bearer token).
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		c.Next()
	}
}

// BodyLimit cuts bodies larger than limit; when reading them the handler gets a
// *http.MaxBytesError that apierror turns into a 413.
func BodyLimit(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > limit {
			apierror.Respond(c, apierror.ErrPayloadTooLarge)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// RateLimiter limits requests per IP in memory. One replica is enough; with several,
// the counter would have to be shared.
type RateLimiter struct {
	perMinute int
	mu        sync.Mutex
	visitors  map[string]*visitor
	now       func() time.Time
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter allows perMinute requests per minute per IP, with bursts of the same size.
func NewRateLimiter(perMinute int) *RateLimiter {
	return &RateLimiter{perMinute: perMinute, visitors: map[string]*visitor{}, now: time.Now}
}

// Middleware returns the Gin handler. The IP is c.ClientIP(), which only trusts
// X-Forwarded-For when the request comes from a trusted proxy (nginx).
func (l *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		limiter := l.limiterFor(c.ClientIP())
		reservation := limiter.Reserve()
		if delay := reservation.Delay(); delay > 0 {
			reservation.Cancel()
			c.Header("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
			apierror.Respond(c, apierror.ErrTooManyRequests)
			return
		}
		c.Next()
	}
}

func (l *RateLimiter) limiterFor(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	// Lazy cleanup: whoever has not shown up for 5 minutes already has a full quota.
	if len(l.visitors) > 10_000 {
		for key, v := range l.visitors {
			if now.Sub(v.lastSeen) > 5*time.Minute {
				delete(l.visitors, key)
			}
		}
	}
	v, ok := l.visitors[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rate.Every(time.Minute/time.Duration(l.perMinute)), l.perMinute)}
		l.visitors[ip] = v
	}
	v.lastSeen = now
	return v.limiter
}
