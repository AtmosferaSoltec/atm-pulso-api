// Package ids generates and validates identifiers.
package ids

import (
	"fmt"

	"github.com/google/uuid"
)

// New generates a UUID v7: time-ordered, which keeps the indexes of growing tables compact.
func New() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// Parse accepts any valid UUID. Clients may generate their own ids and the API
// respects them on create, so retrying a request does not duplicate anything.
func Parse(text string) (uuid.UUID, error) {
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid id %q: %w", text, err)
	}
	if id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("invalid id %q: nil uuid", text)
	}
	return id, nil
}
