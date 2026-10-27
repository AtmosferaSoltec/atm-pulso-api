package domain

import "github.com/google/uuid"

// User is a person who signs in. The scope depends on the role: the superadmin has
// no organization, an owner has the whole organization, an admin has one gym
// (GymID) and a receptionist has assigned branches (kept in user_branches).
type User struct {
	ID uuid.UUID
	// OrganizationID is uuid.Nil for the superadmin.
	OrganizationID uuid.UUID
	// GymID is the gym an admin manages; uuid.Nil for the other roles.
	GymID          uuid.UUID
	DocumentNumber string
	FullName       string
	// Phone is empty when the user did not give one.
	Phone    string
	Role     Role
	IsActive bool
	// PasswordVersion is stamped in every token; a password change bumps it and so
	// signs out all the older tokens.
	PasswordVersion int
}

// Credentials is a user together with the stored password hash. It never leaves
// the app layer.
type Credentials struct {
	User
	PasswordHash string
}
