// Package app holds the use cases of body_tracking. Persistence and HTTP are ports/adapters.
package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Actions and resources written to the access log.
const (
	ActionRead   = "read"
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"

	ResourceMeasurements = "measurements"
	ResourceRoutines     = "routines"
)

// MeasurementInput is the body of POST (and PUT) /members/{id}/measurements. Decimals may come as JSON numbers or
// strings, with at most one decimal digit. A PUT replaces the whole record: absent optional values are cleared.
type MeasurementInput struct {
	// ID is the client id that makes a retry of the POST idempotent.
	ID uuid.UUID `json:"id"`
	// MeasuredOn is YYYY-MM-DD; absent means today in Lima.
	MeasuredOn *string      `json:"measured_on"`
	WeightKg   *json.Number `json:"weight_kg"`
	HeightCm   *json.Number `json:"height_cm"`
	BodyFatPct *json.Number `json:"body_fat_pct"`
	WaistCm    *json.Number `json:"waist_cm"`
	ChestCm    *json.Number `json:"chest_cm"`
	HipCm      *json.Number `json:"hip_cm"`
	Notes      string       `json:"notes"`
}

// MeasurementView is a measurement as the API answers it.
type MeasurementView struct {
	ID            uuid.UUID    `json:"id"`
	MemberID      uuid.UUID    `json:"member_id"`
	MeasuredOn    string       `json:"measured_on"`
	WeightKg      *json.Number `json:"weight_kg"`
	HeightCm      *json.Number `json:"height_cm"`
	BodyFatPct    *json.Number `json:"body_fat_pct"`
	WaistCm       *json.Number `json:"waist_cm"`
	ChestCm       *json.Number `json:"chest_cm"`
	HipCm         *json.Number `json:"hip_cm"`
	Notes         string       `json:"notes"`
	CreatedBy     uuid.UUID    `json:"created_by"`
	CreatedByName string       `json:"created_by_name"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedBy     uuid.UUID    `json:"updated_by"`
	UpdatedByName string       `json:"updated_by_name"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

// RoutineInput is the body of POST (and PUT) /members/{id}/routines.
type RoutineInput struct {
	ID uuid.UUID `json:"id"`
	// StartedOn is YYYY-MM-DD; absent means today in Lima.
	StartedOn *string `json:"started_on"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	Notes     string  `json:"notes"`
	// IsCurrent marks the routine in force; absent means true.
	IsCurrent *bool `json:"is_current"`
}

// RoutineView is a routine as the API answers it.
type RoutineView struct {
	ID            uuid.UUID `json:"id"`
	MemberID      uuid.UUID `json:"member_id"`
	StartedOn     string    `json:"started_on"`
	Title         string    `json:"title"`
	Content       string    `json:"content"`
	Notes         string    `json:"notes"`
	IsCurrent     bool      `json:"is_current"`
	CreatedBy     uuid.UUID `json:"created_by"`
	CreatedByName string    `json:"created_by_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedBy     uuid.UUID `json:"updated_by"`
	UpdatedByName string    `json:"updated_by_name"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AccessEntry is one row of the access log (who touched the health data of a member, never the values).
type AccessEntry struct {
	ID         int64      `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	UserName   string     `json:"user_name"`
	Action     string     `json:"action"`
	Resource   string     `json:"resource"`
	ResourceID *uuid.UUID `json:"resource_id"`
	OccurredAt time.Time  `json:"occurred_at"`
}

// Measurement is a validated measurement ready to store; decimals are text with one digit.
type Measurement struct {
	MeasuredOn                                              time.Time
	WeightKg, HeightCm, BodyFatPct, WaistCm, ChestCm, HipCm *string
	Notes                                                   string
}

// Routine is a validated routine ready to store.
type Routine struct {
	StartedOn             time.Time
	Title, Content, Notes string
	IsCurrent             bool
}

// Actor is who performs the action (for audit and authorship).
type Actor struct{ UserID uuid.UUID }

// Repository is the persistence port. Every method is limited to the gym and, for records, to the member. Reads of the
// health data and writes append their access-log row in the SAME transaction.
type Repository interface {
	// MemberHomeBranch returns the home branch of a member of the gym, or domain.ErrMemberNotFound.
	MemberHomeBranch(ctx context.Context, gymID, memberID uuid.UUID) (uuid.UUID, error)

	Measurements(ctx context.Context, gymID, memberID uuid.UUID, actor Actor) ([]MeasurementView, error)
	CreateMeasurement(ctx context.Context, gymID, memberID uuid.UUID, id uuid.UUID, m Measurement, actor Actor) (MeasurementView, bool, error)
	UpdateMeasurement(ctx context.Context, gymID, memberID, id uuid.UUID, m Measurement, actor Actor) (MeasurementView, error)
	DeleteMeasurement(ctx context.Context, gymID, memberID, id uuid.UUID, actor Actor) error

	Routines(ctx context.Context, gymID, memberID uuid.UUID, actor Actor) ([]RoutineView, error)
	CreateRoutine(ctx context.Context, gymID, memberID uuid.UUID, id uuid.UUID, r Routine, actor Actor) (RoutineView, bool, error)
	UpdateRoutine(ctx context.Context, gymID, memberID, id uuid.UUID, r Routine, actor Actor) (RoutineView, error)
	DeleteRoutine(ctx context.Context, gymID, memberID, id uuid.UUID, actor Actor) error

	AccessLog(ctx context.Context, gymID, memberID uuid.UUID, limit int) ([]AccessEntry, error)
}
