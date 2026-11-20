package app

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

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

// MeasurementReport is the content of the measurements PDF: who, and one merged row per measurement date, newest first.
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
	return MeasurementReport{Member: card, Measurements: MergeByDay(list)}, nil
}

// NotesSeparator joins the distinct notes of the records of one day.
const NotesSeparator = " · "

// MergeByDay collapses the measurements of the same measured_on date into one row, for the exports. The input comes
// newest first (date, then created_at, as the repository lists it) and the output keeps that order of dates. For every
// field the most recent non-empty value of the day wins (by created_at); the notes are the distinct non-empty notes in
// chronological order joined by NotesSeparator. Identity fields (id, authorship, timestamps) are those of the latest
// record of the day. The input is not modified.
func MergeByDay(newestFirst []MeasurementView) []MeasurementView {
	out := make([]MeasurementView, 0, len(newestFirst))
	for i := 0; i < len(newestFirst); {
		j := i
		for j < len(newestFirst) && newestFirst[j].MeasuredOn == newestFirst[i].MeasuredOn {
			j++
		}
		out = append(out, mergeDay(newestFirst[i:j]))
		i = j
	}
	return out
}

// mergeDay merges the records of one date, given newest first.
func mergeDay(day []MeasurementView) MeasurementView {
	if len(day) == 1 {
		return day[0]
	}
	chronological := make([]MeasurementView, len(day))
	for i, m := range day {
		chronological[len(day)-1-i] = m
	}
	sort.SliceStable(chronological, func(a, b int) bool { return chronological[a].CreatedAt.Before(chronological[b].CreatedAt) })

	merged := chronological[len(chronological)-1]
	merged.Notes = ""
	pick := func(dst **json.Number, get func(MeasurementView) *json.Number) {
		*dst = nil
		for _, m := range chronological {
			if v := get(m); v != nil {
				*dst = v
			}
		}
	}
	pick(&merged.WeightKg, func(m MeasurementView) *json.Number { return m.WeightKg })
	pick(&merged.HeightCm, func(m MeasurementView) *json.Number { return m.HeightCm })
	pick(&merged.BodyFatPct, func(m MeasurementView) *json.Number { return m.BodyFatPct })
	pick(&merged.WaistCm, func(m MeasurementView) *json.Number { return m.WaistCm })
	pick(&merged.ChestCm, func(m MeasurementView) *json.Number { return m.ChestCm })
	pick(&merged.HipCm, func(m MeasurementView) *json.Number { return m.HipCm })

	var notes []string
	seen := map[string]bool{}
	for _, m := range chronological {
		n := strings.TrimSpace(m.Notes)
		if n != "" && !seen[n] {
			seen[n] = true
			notes = append(notes, n)
		}
	}
	merged.Notes = strings.Join(notes, NotesSeparator)
	return merged
}
