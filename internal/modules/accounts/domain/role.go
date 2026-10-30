// Package domain holds the entities and rules of the accounts module. Plain Go: it
// imports neither gin nor pgx nor net/http.
package domain

// Role is what a person is allowed to be in Pulso. It is the `app_role` of atm-auth-api.
type Role string

// The two roles of Pulso.
const (
	RoleAdmin        Role = "admin"
	RoleReceptionist Role = "receptionist"
)

// Valid says whether r is one of the two roles.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleReceptionist }
