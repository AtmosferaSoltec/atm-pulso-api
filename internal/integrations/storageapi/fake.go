package storageapi

import (
	"context"
	"sync"
	"time"
)

// Fake answers links from memory, for tests. It is never used in production.
type Fake struct {
	mu    sync.Mutex
	links map[string]Link
	// Err, when set, is returned by every call (to test an outage).
	Err error
	// Calls counts the requests, to test caching.
	Calls int
}

// NewFake returns an empty fake.
func NewFake() *Fake { return &Fake{links: map[string]Link{}} }

// SetLink makes fileID readable, with a link that expires at expiresAt.
func (f *Fake) SetLink(fileID, url string, expiresAt time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.links[fileID] = Link{URL: url, ExpiresAt: expiresAt}
}

// PreviewLinks implements Client.
func (f *Fake) PreviewLinks(_ context.Context, fileIDs []string) (map[string]Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Err != nil {
		return nil, f.Err
	}
	out := map[string]Link{}
	for _, id := range fileIDs {
		if link, ok := f.links[id]; ok {
			out[id] = link
		}
	}
	return out, nil
}
