// Package storageapi is the client of atm-storage-api that Pulso's backend uses with its own service
// token, only to read company logos: Pulso never uploads, serves or processes those files, it asks
// storage for short-lived signed links and hands them to the clients.
package storageapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Link is a temporary address that shows an image.
type Link struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Client is what Pulso needs from atm-storage-api.
type Client interface {
	// PreviewLinks returns inline links for the given file ids. Ids the service token cannot read, that do not
	// exist or are not images are left out of the answer.
	PreviewLinks(ctx context.Context, fileIDs []string) (map[string]Link, error)
}

// ErrUnavailable: atm-storage-api did not answer in time, refused our token or answered a server error.
var ErrUnavailable = errors.New("atm-storage-api unavailable")

// TokenSource gives the service token of the system (atm-auth-api issues it).
type TokenSource interface {
	ServiceToken(ctx context.Context) (string, error)
	// ForgetToken is called when storage refused the token, so the next call asks for a new one.
	ForgetToken()
}

// HTTPClient talks to atm-storage-api.
type HTTPClient struct {
	baseURL string
	tokens  TokenSource
	http    *http.Client
}

// NewHTTPClient returns a client. timeout bounds every call.
func NewHTTPClient(baseURL string, tokens TokenSource, timeout time.Duration) *HTTPClient {
	return &HTTPClient{baseURL: baseURL, tokens: tokens, http: &http.Client{Timeout: timeout}}
}

// PreviewLinks implements Client with POST /v1/files/preview.
func (c *HTTPClient) PreviewLinks(ctx context.Context, fileIDs []string) (map[string]Link, error) {
	if len(fileIDs) == 0 {
		return map[string]Link{}, nil
	}
	token, err := c.tokens.ServiceToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: service token: %v", ErrUnavailable, err)
	}
	raw, err := json.Marshal(map[string][]string{"ids": fileIDs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/files/preview", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusUnauthorized {
		c.tokens.ForgetToken()
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrUnavailable, res.StatusCode)
	}
	var out struct {
		Items map[string]Link `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrUnavailable, err)
	}
	if out.Items == nil {
		out.Items = map[string]Link{}
	}
	return out.Items, nil
}
