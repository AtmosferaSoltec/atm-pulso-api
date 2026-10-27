// Package identity is the client of atm-identity-api, Atmósfera's internal service that
// resolves Peruvian documents (DNI -> person, RUC -> company). Pulso never calls the
// paid upstream directly and keeps no copy of these data: this service is the cache.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrNotFound: the document does not exist (404 from identity-api).
	ErrNotFound = errors.New("document not found")
	// ErrUnavailable: identity-api or its upstream failed, timed out or is unreachable.
	ErrUnavailable = errors.New("identity service unavailable")
	// ErrRejected: identity-api refused our API key. It is a configuration problem,
	// not the user's, so callers report it as unavailable and the operator reads the log.
	ErrRejected = errors.New("identity service rejected the api key")
)

// Person is what identity-api knows about a DNI. Names are split, never one string.
type Person struct {
	DocumentNumber  string
	FirstNames      string
	PaternalSurname string
	MaternalSurname string
}

// Company is what identity-api knows about a RUC.
type Company struct {
	DocumentNumber string
	LegalName      string
	TradeName      string
	Address        string
}

// Client is the common contract of the real HTTP client and of the development fake.
type Client interface {
	Person(ctx context.Context, documentNumber string) (Person, error)
	Company(ctx context.Context, documentNumber string) (Company, error)
}

// HTTPClient calls identity-api with the X-API-Key of this system.
type HTTPClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewHTTPClient builds the client. timeout bounds each call: a slow upstream must not
// hold a request of ours.
func NewHTTPClient(baseURL, apiKey string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: timeout},
	}
}

type personBody struct {
	DocumentNumber  string `json:"document_number"`
	FirstNames      string `json:"first_names"`
	PaternalSurname string `json:"paternal_surname"`
	MaternalSurname string `json:"maternal_surname"`
}

type companyBody struct {
	DocumentNumber string `json:"document_number"`
	LegalName      string `json:"legal_name"`
	TradeName      string `json:"trade_name"`
	Address        string `json:"address"`
}

type envelope[T any] struct {
	Data T `json:"data"`
}

// Person resolves a DNI.
func (c *HTTPClient) Person(ctx context.Context, documentNumber string) (Person, error) {
	body, err := get[personBody](ctx, c, "/v1/persons/"+url.PathEscape(documentNumber))
	if err != nil {
		return Person{}, err
	}
	return Person(body), nil
}

// Company resolves a RUC.
func (c *HTTPClient) Company(ctx context.Context, documentNumber string) (Company, error) {
	body, err := get[companyBody](ctx, c, "/v1/companies/"+url.PathEscape(documentNumber))
	if err != nil {
		return Company{}, err
	}
	return Company(body), nil
}

func get[T any](ctx context.Context, c *HTTPClient, path string) (T, error) {
	var zero T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return zero, fmt.Errorf("build identity request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		// The error text of net/http may carry the URL; it holds no secret, but a
		// document number is personal data, so only the kind of failure is kept.
		return zero, fmt.Errorf("%w: request failed", ErrUnavailable)
	}
	defer func() { _ = res.Body.Close() }()

	switch res.StatusCode {
	case http.StatusOK:
		var decoded envelope[T]
		if err := json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&decoded); err != nil {
			return zero, fmt.Errorf("%w: unreadable answer", ErrUnavailable)
		}
		return decoded.Data, nil
	case http.StatusNotFound:
		return zero, ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return zero, ErrRejected
	default:
		return zero, fmt.Errorf("%w: status %d", ErrUnavailable, res.StatusCode)
	}
}
