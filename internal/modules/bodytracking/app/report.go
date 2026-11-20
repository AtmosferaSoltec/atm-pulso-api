package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// MemberCard identifies the member on the measurements PDF.
type MemberCard struct {
	FullName       string
	DocumentType   string
	DocumentNumber string
}

// Cards reads who a member is (the members table, read-only).
type Cards interface {
	// MemberCard returns the member of the gym, or domain.ErrMemberNotFound.
	MemberCard(ctx context.Context, gymID, memberID uuid.UUID) (MemberCard, error)
}

// MeasurementReport is the content of the measurements PDF: who, and every live measurement newest first.
type MeasurementReport struct {
	Member       MemberCard
	Measurements []MeasurementView
}

// MeasurementReport gathers the PDF of a member's measurements. It goes through exactly the same checks as
// Measurements (role, gym, branch) and the same read, so the access log records it like any other read.
func (s *Service) MeasurementReport(ctx context.Context, p auth.Principal, memberID uuid.UUID) (MeasurementReport, error) {
	actor, err := s.member(ctx, p, memberID)
	if err != nil {
		return MeasurementReport{}, err
	}
	card, err := s.Cards.MemberCard(ctx, p.GymID, memberID)
	if err != nil {
		return MeasurementReport{}, err
	}
	list, err := s.Repo.Measurements(ctx, p.GymID, memberID, actor)
	if err != nil {
		return MeasurementReport{}, err
	}
	return MeasurementReport{Member: card, Measurements: list}, nil
}
