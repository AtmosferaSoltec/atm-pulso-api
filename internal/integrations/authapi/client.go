// Package authapi is the client of atm-auth-api that Pulso's backend uses with its own service
// token: to read the company a token belongs to and to manage the members of that company.
// Who may do what is decided by Pulso; the service token only proves which system is asking.
package authapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Tenant is the company profile atm-auth-api keeps; Pulso only needs its names.
type Tenant struct {
	ID          string `json:"id"`
	RUC         string `json:"ruc"`
	LegalName   string `json:"legal_name"`
	TradeName   string `json:"trade_name"`
	DisplayName string `json:"display_name"`
	// ClientID and ClientName are the customer that owns the company: a Pulso gym.
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`
	// LogoFileID is the id of the company logo in atm-storage-api; nil while it has none. It is an id,
	// never an address: ask atm-storage-api for a temporary link.
	LogoFileID *string `json:"logo_file_id"`
}

// Name is the best name the company has.
func (t Tenant) Name() string {
	for _, name := range []string{t.DisplayName, t.TradeName, t.LegalName, t.RUC} {
		if name != "" {
			return name
		}
	}
	return ""
}

// Member is a person with access to a company in Pulso.
type Member struct {
	UserID      string `json:"user_id"`
	DNI         string `json:"dni"`
	FullName    string `json:"full_name"`
	Role        string `json:"role"`
	ManageUsers bool   `json:"manage_users"`
	Status      string `json:"status"`
}

// NewMember is a person to add to a company. If the DNI already has an account only the access is added.
type NewMember struct {
	DNI      string `json:"dni"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
}

// Client is what Pulso needs from atm-auth-api.
type Client interface {
	// Tenants are the companies that have Pulso enabled.
	Tenants(ctx context.Context) ([]Tenant, error)
	// Tenant returns ErrNotFound when the company is not served.
	Tenant(ctx context.Context, tenantID string) (Tenant, error)
	Members(ctx context.Context, tenantID string) ([]Member, error)
	AddMember(ctx context.Context, tenantID string, in NewMember) (Member, error)
	ChangeRole(ctx context.Context, tenantID, userID, role string) error
	ResetPassword(ctx context.Context, tenantID, userID string) error
}

// Error is an answer of atm-auth-api that is not a success.
type Error struct {
	Status int
	// Code is the stable snake_case code of atm-auth-api (`protected_member`, ...).
	Code string
}

func (e *Error) Error() string { return fmt.Sprintf("atm-auth-api answered %d %s", e.Status, e.Code) }

var (
	// ErrNotFound: the company or the member does not exist for this system.
	ErrNotFound = errors.New("not found")
	// ErrUnavailable: atm-auth-api did not answer in time or answered a server error.
	ErrUnavailable = errors.New("atm-auth-api unavailable")
)

// HTTPClient talks to atm-auth-api with the system's service token.
type HTTPClient struct {
	baseURL, systemCode, secret string
	http                        *http.Client
	now                         func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewHTTPClient returns a client. timeout bounds every call.
func NewHTTPClient(baseURL, systemCode, secret string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{baseURL: baseURL, systemCode: systemCode, secret: secret, http: &http.Client{Timeout: timeout}, now: time.Now}
}

// serviceToken returns a cached token, asking for a new one a minute before it expires.
func (c *HTTPClient) serviceToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Add(time.Minute).Before(c.expires) {
		return c.token, nil
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/token", "", map[string]string{"client_id": c.systemCode, "client_secret": c.secret}, &out); err != nil {
		return "", err
	}
	c.token, c.expires = out.AccessToken, c.now().Add(time.Duration(out.ExpiresIn)*time.Second)
	return c.token, nil
}

// ServiceToken is the system's service token (cached), for the other Atmosfera services that accept it,
// such as atm-storage-api.
func (c *HTTPClient) ServiceToken(ctx context.Context) (string, error) { return c.serviceToken(ctx) }

// ForgetToken drops the cached service token, for a caller that was refused with it.
func (c *HTTPClient) ForgetToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

func (c *HTTPClient) authorized(ctx context.Context, method, path string, body, out any) error {
	token, err := c.serviceToken(ctx)
	if err != nil {
		return err
	}
	err = c.do(ctx, method, path, token, body, out)
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		// The token was revoked or the key rotated: forget it so the next call asks again.
		c.mu.Lock()
		c.token = ""
		c.mu.Unlock()
	}
	return err
}

func (c *HTTPClient) do(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 500 {
		return fmt.Errorf("%w: status %d", ErrUnavailable, res.StatusCode)
	}
	if res.StatusCode >= 400 {
		var failure struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&failure)
		return &Error{Status: res.StatusCode, Code: failure.Error.Code}
	}
	if out == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrUnavailable, err)
	}
	return nil
}

func tenantQuery(tenantID string) string { return "?tenant_id=" + url.QueryEscape(tenantID) }

// Tenants lists the customers of the system: the companies that have it enabled.
func (c *HTTPClient) Tenants(ctx context.Context) ([]Tenant, error) {
	var out struct {
		Items []Tenant `json:"items"`
	}
	if err := c.authorized(ctx, http.MethodGet, "/v1/service/tenants", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// Tenant finds one company among the customers of the system.
func (c *HTTPClient) Tenant(ctx context.Context, tenantID string) (Tenant, error) {
	items, err := c.Tenants(ctx)
	if err != nil {
		return Tenant{}, err
	}
	for _, tenant := range items {
		if tenant.ID == tenantID {
			return tenant, nil
		}
	}
	return Tenant{}, ErrNotFound
}

// Members lists the people of a company in this system.
func (c *HTTPClient) Members(ctx context.Context, tenantID string) ([]Member, error) {
	var out struct {
		Items []Member `json:"items"`
	}
	if err := c.authorized(ctx, http.MethodGet, "/v1/service/members"+tenantQuery(tenantID), nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// AddMember creates the account (the DNI is the first password) or only adds the access.
func (c *HTTPClient) AddMember(ctx context.Context, tenantID string, in NewMember) (Member, error) {
	var out Member
	if err := c.authorized(ctx, http.MethodPost, "/v1/service/members"+tenantQuery(tenantID), in, &out); err != nil {
		return Member{}, err
	}
	return out, nil
}

// ChangeRole gives an existing member another role of the system.
func (c *HTTPClient) ChangeRole(ctx context.Context, tenantID, userID, role string) error {
	return c.authorized(ctx, http.MethodPut, "/v1/service/members/"+url.PathEscape(userID)+tenantQuery(tenantID), map[string]string{"role": role}, nil)
}

// ResetPassword sends the member back to the DNI as password.
func (c *HTTPClient) ResetPassword(ctx context.Context, tenantID, userID string) error {
	return c.authorized(ctx, http.MethodPost, "/v1/service/members/"+url.PathEscape(userID)+"/reset-password"+tenantQuery(tenantID), nil, nil)
}
