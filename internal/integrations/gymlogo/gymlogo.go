// Package gymlogo resolves the logo of a gym. The logo belongs to the company profile of atm-auth-api
// (`logo_file_id`) and the bytes to atm-storage-api; Pulso stores nothing and serves nothing: it
// exchanges the file id for a short-lived signed link and hands that link to its clients.
//
// The answer is cached briefly (a link is reused until shortly before it expires) and the resolver
// degrades safely: when atm-auth-api or atm-storage-api fail, or the gym has no logo, the answer is
// nil and the rest of the request goes on. A failure is remembered for a few seconds so an outage
// does not slow every request.
package gymlogo

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/storageapi"
)

const (
	// DefaultTTL is how long the logo of a gym is remembered, so a change made in console-web shows up soon.
	DefaultTTL = 5 * time.Minute
	// DefaultFailureTTL is how long a failed resolution is remembered.
	DefaultFailureTTL = 30 * time.Second
	// DefaultTimeout bounds one resolution (both services together).
	DefaultTimeout = 3 * time.Second
	// linkMargin: a link that expires sooner than this is never handed out.
	linkMargin = time.Minute
)

// Resolver turns a gym (its company id in atm-auth-api) into the address of its logo.
type Resolver struct {
	tenants authapi.Client
	storage storageapi.Client
	log     *slog.Logger
	now     func() time.Time
	ttl     time.Duration
	failTTL time.Duration
	timeout time.Duration

	mu    sync.Mutex
	cache map[uuid.UUID]entry
	group singleflight.Group
}

type entry struct {
	url     *string
	expires time.Time
}

// New builds a resolver. Either dependency may fail at any time; the resolver never returns an error.
func New(tenants authapi.Client, storage storageapi.Client, log *slog.Logger) *Resolver {
	return &Resolver{
		tenants: tenants, storage: storage, log: log, now: time.Now,
		ttl: DefaultTTL, failTTL: DefaultFailureTTL, timeout: DefaultTimeout,
		cache: map[uuid.UUID]entry{},
	}
}

// LogoURL returns a temporary address for the logo of the gym whose company is tenantID, or nil when the gym
// has no logo or it cannot be resolved right now. The caller passes the tenant of the verified principal,
// never one taken from a request, so a gym can only ever see its own logo.
func (r *Resolver) LogoURL(ctx context.Context, tenantID uuid.UUID) *string {
	if r == nil || tenantID == uuid.Nil {
		return nil
	}
	if url, ok := r.cached(tenantID); ok {
		return url
	}
	// One resolution per gym at a time; it runs detached from the request so a client that gives up does
	// not poison the cache, and it is bounded by its own timeout.
	value, _, _ := r.group.Do(tenantID.String(), func() (any, error) {
		resolveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.timeout)
		defer cancel()
		url, expires, err := r.resolve(resolveCtx, tenantID)
		if err != nil {
			r.log.Warn("gym logo unavailable", "error", err)
			r.store(tenantID, nil, r.now().Add(r.failTTL))
			return (*string)(nil), nil
		}
		r.store(tenantID, url, expires)
		return url, nil
	})
	url, _ := value.(*string)
	return url
}

func (r *Resolver) cached(tenantID uuid.UUID) (*string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.cache[tenantID]
	if !ok || !r.now().Before(e.expires) {
		return nil, false
	}
	return e.url, true
}

func (r *Resolver) store(tenantID uuid.UUID, url *string, expires time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[tenantID] = entry{url: url, expires: expires}
}

// resolve asks atm-auth-api for the file id and atm-storage-api for the link. It returns how long the
// answer may be reused.
func (r *Resolver) resolve(ctx context.Context, tenantID uuid.UUID) (*string, time.Time, error) {
	tenant, err := r.tenants.Tenant(ctx, tenantID.String())
	if errors.Is(err, authapi.ErrNotFound) {
		return nil, r.now().Add(r.ttl), nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	if tenant.LogoFileID == nil || *tenant.LogoFileID == "" {
		return nil, r.now().Add(r.ttl), nil
	}
	links, err := r.storage.PreviewLinks(ctx, []string{*tenant.LogoFileID})
	if err != nil {
		return nil, time.Time{}, err
	}
	link, ok := links[*tenant.LogoFileID]
	if !ok || link.URL == "" {
		// The file is gone, is not an image or the service cannot read it: no logo, retry soon.
		return nil, r.now().Add(r.failTTL), nil
	}
	expires := r.now().Add(r.ttl)
	if limit := link.ExpiresAt.Add(-linkMargin); !link.ExpiresAt.IsZero() && limit.Before(expires) {
		expires = limit
	}
	url := link.URL
	return &url, expires, nil
}
