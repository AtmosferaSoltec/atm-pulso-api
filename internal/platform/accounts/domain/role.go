// Package domain holds the entities and rules of the accounts module. Plain Go: it
// imports neither gin nor pgx nor net/http.
package domain

// Role is what a user is allowed to be.
type Role string

// The four roles of Pulso.
const (
	RoleSuperadmin   Role = "superadmin"
	RoleOwner        Role = "owner"
	RoleAdmin        Role = "admin"
	RoleReceptionist Role = "receptionist"
)

// Valid says whether r is one of the four roles.
func (r Role) Valid() bool {
	switch r {
	case RoleSuperadmin, RoleOwner, RoleAdmin, RoleReceptionist:
		return true
	default:
		return false
	}
}
