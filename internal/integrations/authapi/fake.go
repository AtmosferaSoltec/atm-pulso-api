package authapi

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Fake keeps companies and members in memory, for development without atm-auth-api and for tests.
// It is never used in production (the config requires the service secret there).
type Fake struct {
	mu      sync.Mutex
	tenants map[string]Tenant
	members map[string][]Member
	// Resets records the (tenant, user) pairs whose password was reset.
	Resets [][2]string
	// Err, when set, is returned by every call (to test an outage).
	Err error
}

// NewFake returns an empty fake.
func NewFake() *Fake {
	return &Fake{tenants: map[string]Tenant{}, members: map[string][]Member{}}
}

// SetTenant registers a company.
func (f *Fake) SetTenant(tenant Tenant) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tenants[tenant.ID] = tenant
}

// SetMember registers a person of a company with a known user id.
func (f *Fake) SetMember(tenantID string, member Member) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members[tenantID] = append(f.members[tenantID], member)
}

// Tenants implements Client.
func (f *Fake) Tenants(_ context.Context) ([]Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	out := make([]Tenant, 0, len(f.tenants))
	for _, tenant := range f.tenants {
		out = append(out, tenant)
	}
	return out, nil
}

// Tenant implements Client.
func (f *Fake) Tenant(_ context.Context, tenantID string) (Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return Tenant{}, f.Err
	}
	tenant, ok := f.tenants[tenantID]
	if !ok {
		return Tenant{}, ErrNotFound
	}
	return tenant, nil
}

// Members implements Client.
func (f *Fake) Members(_ context.Context, tenantID string) ([]Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]Member{}, f.members[tenantID]...), nil
}

// AddMember implements Client. The same DNI keeps the same user id in every company.
func (f *Fake) AddMember(_ context.Context, tenantID string, in NewMember) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return Member{}, f.Err
	}
	for _, member := range f.members[tenantID] {
		if member.DNI == in.DNI {
			return member, nil
		}
	}
	id := uuid.NewSHA1(uuid.NameSpaceDNS, []byte("dni:"+in.DNI)).String()
	member := Member{UserID: id, DNI: in.DNI, FullName: in.FullName, Role: in.Role, Status: "active"}
	f.members[tenantID] = append(f.members[tenantID], member)
	return member, nil
}

// ChangeRole implements Client.
func (f *Fake) ChangeRole(_ context.Context, tenantID, userID, role string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	for i, member := range f.members[tenantID] {
		if member.UserID == userID {
			if member.ManageUsers {
				return &Error{Status: 403, Code: "protected_member"}
			}
			f.members[tenantID][i].Role = role
			return nil
		}
	}
	return &Error{Status: 404, Code: "member_not_found"}
}

// ResetPassword implements Client.
func (f *Fake) ResetPassword(_ context.Context, tenantID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	for _, member := range f.members[tenantID] {
		if member.UserID == userID {
			f.Resets = append(f.Resets, [2]string{tenantID, userID})
			return nil
		}
	}
	return &Error{Status: 404, Code: "member_not_found"}
}
