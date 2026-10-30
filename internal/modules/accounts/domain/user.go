package domain

import "github.com/google/uuid"

// User is a person who works in one gym. The same person in atm-auth-api (AuthUserID) has
// one User per gym they work for. An admin reaches every branch of the gym; a receptionist
// has assigned branches (kept in user_branches).
type User struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	GymID          uuid.UUID
	AuthUserID     uuid.UUID
	DocumentNumber string
	FullName       string
	// Phone is empty when the user did not give one.
	Phone    string
	Role     Role
	IsActive bool
}
