package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

// MaxAccessLog is how many rows GET .../body-access-log answers (the newest).
const MaxAccessLog = 200

// Service implements the use cases. The module switch (FEATURE_DISABLED) is a route guard, not here.
type Service struct {
	Repo  Repository
	Clock clock.Clock
}

func (s *Service) today() time.Time { return clock.LimaDate(s.Clock.Now()) }

func staffOnly(p auth.Principal) error {
	if p.Role != auth.RoleAdmin && p.Role != auth.RoleReceptionist {
		return domain.ErrForbidden
	}
	return nil
}

// member checks the role, that the member belongs to the gym of the token (another gym: not found, even inside the
// same group) and that the staff member can reach the member's home branch.
func (s *Service) member(ctx context.Context, p auth.Principal, memberID uuid.UUID) (Actor, error) {
	if err := staffOnly(p); err != nil {
		return Actor{}, err
	}
	branchID, err := s.Repo.MemberHomeBranch(ctx, p.GymID, memberID)
	if err != nil {
		return Actor{}, err
	}
	if !p.CanAccessBranch(auth.BranchRef{ID: branchID, GymID: p.GymID}) {
		return Actor{}, domain.ErrBranchNotAccessible
	}
	return Actor{UserID: p.UserID}, nil
}

func (s *Service) date(raw *string, field string, issues *[]domain.Issue) time.Time {
	today := s.today()
	if raw == nil {
		return today
	}
	date, err := time.Parse("2006-01-02", *raw)
	if err != nil {
		*issues = append(*issues, domain.Issue{Field: field, Code: domain.CodeDate})
		return today
	}
	if code, ok := domain.CheckDate(date, today); !ok {
		*issues = append(*issues, domain.Issue{Field: field, Code: code})
	}
	return date
}

func tenths(raw *json.Number, field, code string, r domain.Range, issues *[]domain.Issue) *string {
	if raw == nil {
		return nil
	}
	value, ok := domain.ParseTenths(raw.String(), r)
	if !ok {
		*issues = append(*issues, domain.Issue{Field: field, Code: code})
		return nil
	}
	text := domain.FormatTenths(value)
	return &text
}

func validateMeasurement(s *Service, in MeasurementInput) (Measurement, error) {
	var issues []domain.Issue
	m := Measurement{MeasuredOn: s.date(in.MeasuredOn, "measured_on", &issues)}
	m.WeightKg = tenths(in.WeightKg, "weight_kg", domain.CodeWeight, domain.WeightKg, &issues)
	m.HeightCm = tenths(in.HeightCm, "height_cm", domain.CodeHeight, domain.HeightCm, &issues)
	m.BodyFatPct = tenths(in.BodyFatPct, "body_fat_pct", domain.CodeBodyFat, domain.BodyFatPct, &issues)
	m.WaistCm = tenths(in.WaistCm, "waist_cm", domain.CodeWaist, domain.GirthCm, &issues)
	m.ChestCm = tenths(in.ChestCm, "chest_cm", domain.CodeChest, domain.GirthCm, &issues)
	m.HipCm = tenths(in.HipCm, "hip_cm", domain.CodeHip, domain.GirthCm, &issues)
	if in.WeightKg == nil && in.HeightCm == nil && in.BodyFatPct == nil && in.WaistCm == nil && in.ChestCm == nil && in.HipCm == nil {
		issues = append(issues, domain.Issue{Field: "weight_kg", Code: domain.CodeValuesNeeded})
	}
	notes, ok := domain.Text(in.Notes, domain.MaxNotesLength)
	if !ok {
		issues = append(issues, domain.Issue{Field: "notes", Code: domain.CodeNotes})
	}
	m.Notes = notes
	if len(issues) > 0 {
		return Measurement{}, &domain.ValidationError{Issues: issues}
	}
	return m, nil
}

func validateRoutine(s *Service, in RoutineInput) (Routine, error) {
	var issues []domain.Issue
	r := Routine{StartedOn: s.date(in.StartedOn, "started_on", &issues), IsCurrent: in.IsCurrent == nil || *in.IsCurrent}
	var ok bool
	if r.Title, ok = domain.Title(in.Title); !ok {
		issues = append(issues, domain.Issue{Field: "title", Code: domain.CodeTitle})
	}
	if r.Content, ok = domain.Text(in.Content, domain.MaxContentLength); !ok {
		issues = append(issues, domain.Issue{Field: "content", Code: domain.CodeContent})
	}
	if r.Notes, ok = domain.Text(in.Notes, domain.MaxNotesLength); !ok {
		issues = append(issues, domain.Issue{Field: "notes", Code: domain.CodeNotes})
	}
	if len(issues) > 0 {
		return Routine{}, &domain.ValidationError{Issues: issues}
	}
	return r, nil
}

func newID(id uuid.UUID) uuid.UUID {
	if id == uuid.Nil {
		return ids.New()
	}
	return id
}

// ---- Measurements ----

// Measurements lists the measurements of a member, newest first. The read is recorded in the access log.
func (s *Service) Measurements(ctx context.Context, p auth.Principal, memberID uuid.UUID) ([]MeasurementView, error) {
	actor, err := s.member(ctx, p, memberID)
	if err != nil {
		return nil, err
	}
	return s.Repo.Measurements(ctx, p.GymID, memberID, actor)
}

// CreateMeasurement stores a measurement; a retry with the same id answers the stored one (existed = true).
func (s *Service) CreateMeasurement(ctx context.Context, p auth.Principal, memberID uuid.UUID, in MeasurementInput) (MeasurementView, bool, error) {
	actor, err := s.member(ctx, p, memberID)
	if err != nil {
		return MeasurementView{}, false, err
	}
	m, err := validateMeasurement(s, in)
	if err != nil {
		return MeasurementView{}, false, err
	}
	return s.Repo.CreateMeasurement(ctx, p.GymID, memberID, newID(in.ID), m, actor)
}

// UpdateMeasurement replaces the values of a measurement.
func (s *Service) UpdateMeasurement(ctx context.Context, p auth.Principal, memberID, id uuid.UUID, in MeasurementInput) (MeasurementView, error) {
	actor, err := s.member(ctx, p, memberID)
	if err != nil {
		return MeasurementView{}, err
	}
	m, err := validateMeasurement(s, in)
	if err != nil {
		return MeasurementView{}, err
	}
	return s.Repo.UpdateMeasurement(ctx, p.GymID, memberID, id, m, actor)
}

// DeleteMeasurement removes a measurement logically (who and when are kept).
func (s *Service) DeleteMeasurement(ctx context.Context, p auth.Principal, memberID, id uuid.UUID) error {
	actor, err := s.member(ctx, p, memberID)
	if err != nil {
		return err
	}
	return s.Repo.DeleteMeasurement(ctx, p.GymID, memberID, id, actor)
}

// ---- Routines ----

// Routines lists the routines of a member: the current ones first, then by start date. The read is logged.
func (s *Service) Routines(ctx context.Context, p auth.Principal, memberID uuid.UUID) ([]RoutineView, error) {
	actor, err := s.member(ctx, p, memberID)
	if err != nil {
		return nil, err
	}
	return s.Repo.Routines(ctx, p.GymID, memberID, actor)
}

// CreateRoutine stores a routine; a retry with the same id answers the stored one.
func (s *Service) CreateRoutine(ctx context.Context, p auth.Principal, memberID uuid.UUID, in RoutineInput) (RoutineView, bool, error) {
	actor, err := s.member(ctx, p, memberID)
	if err != nil {
		return RoutineView{}, false, err
	}
	r, err := validateRoutine(s, in)
	if err != nil {
		return RoutineView{}, false, err
	}
	return s.Repo.CreateRoutine(ctx, p.GymID, memberID, newID(in.ID), r, actor)
}

// UpdateRoutine replaces a routine.
func (s *Service) UpdateRoutine(ctx context.Context, p auth.Principal, memberID, id uuid.UUID, in RoutineInput) (RoutineView, error) {
	actor, err := s.member(ctx, p, memberID)
	if err != nil {
		return RoutineView{}, err
	}
	r, err := validateRoutine(s, in)
	if err != nil {
		return RoutineView{}, err
	}
	return s.Repo.UpdateRoutine(ctx, p.GymID, memberID, id, r, actor)
}

// DeleteRoutine removes a routine logically.
func (s *Service) DeleteRoutine(ctx context.Context, p auth.Principal, memberID, id uuid.UUID) error {
	actor, err := s.member(ctx, p, memberID)
	if err != nil {
		return err
	}
	return s.Repo.DeleteRoutine(ctx, p.GymID, memberID, id, actor)
}

// ---- Access log ----

// AccessLog answers who read or wrote the health data of a member. Admin only; reading it is not itself logged
// because it exposes no health data.
func (s *Service) AccessLog(ctx context.Context, p auth.Principal, memberID uuid.UUID) ([]AccessEntry, error) {
	if p.Role != auth.RoleAdmin {
		return nil, domain.ErrForbidden
	}
	if _, err := s.Repo.MemberHomeBranch(ctx, p.GymID, memberID); err != nil {
		return nil, err
	}
	return s.Repo.AccessLog(ctx, p.GymID, memberID, MaxAccessLog)
}
