package app

import (
	"sync"
	"time"
)

const (
	maxFailures   = 5
	attemptWindow = 15 * time.Minute
)

// attempts counts failed logins (and failed password changes) per key, in memory.
// Nothing is stored in the database: after a restart the counters start over, which
// is acceptable for a brake against guessing. With several replicas the counters
// would have to be shared.
type attempts struct {
	mu      sync.Mutex
	entries map[string]*attemptEntry
}

type attemptEntry struct {
	failures int
	since    time.Time
}

func newAttempts() *attempts {
	return &attempts{entries: map[string]*attemptEntry{}}
}

// lockedFor returns how long key stays locked (zero when it is not).
func (a *attempts) lockedFor(key string, now time.Time) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.entries[key]
	if !ok {
		return 0
	}
	if now.Sub(entry.since) >= attemptWindow {
		delete(a.entries, key)
		return 0
	}
	if entry.failures >= maxFailures {
		return entry.since.Add(attemptWindow).Sub(now)
	}
	return 0
}

// fail records a failed attempt. The window starts at the first failure.
func (a *attempts) fail(key string, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.entries[key]
	if !ok || now.Sub(entry.since) >= attemptWindow {
		a.entries[key] = &attemptEntry{failures: 1, since: now}
		return
	}
	entry.failures++
}

// reset forgets key after a success.
func (a *attempts) reset(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.entries, key)
}
