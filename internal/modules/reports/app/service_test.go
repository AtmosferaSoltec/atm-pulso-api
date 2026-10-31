package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

type fakeDirectory struct{ branches []branchdir.Info }

func (f fakeDirectory) Branch(context.Context, uuid.UUID, uuid.UUID) (branchdir.Info, error) {
	return branchdir.Info{}, branchdir.ErrNotFound
}
func (f fakeDirectory) Branches(context.Context, uuid.UUID) ([]branchdir.Info, error) {
	return f.branches, nil
}

type fakeRepo struct{ rows int }

func (f fakeRepo) Payments(_ context.Context, _ uuid.UUID, filter PaymentFilter) ([]PaymentRow, int, error) {
	n := min(f.rows, filter.Limit)
	return make([]PaymentRow, n), f.rows, nil
}
func (f fakeRepo) Totals(context.Context, uuid.UUID, PaymentFilter) (Totals, error) {
	return Totals{}, nil
}
func (f fakeRepo) Dashboard(context.Context, uuid.UUID, []uuid.UUID, time.Time, time.Time) (DashboardView, error) {
	return DashboardView{}, nil
}

func newService(rows int) (*Service, auth.Principal, auth.Principal) {
	gym, branch := uuid.New(), uuid.New()
	s := &Service{
		Repo:      fakeRepo{rows: rows},
		Directory: fakeDirectory{branches: []branchdir.Info{{ID: branch, GymID: gym}}},
		Clock:     clock.Fixed{At: time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)},
	}
	return s, auth.Principal{GymID: gym, Role: auth.RoleAdmin}, auth.Principal{GymID: gym, Role: auth.RoleReceptionist, BranchIDs: []uuid.UUID{branch}}
}

func TestExportRefusesMoreThanTheLimitAndReception(t *testing.T) {
	s, owner, reception := newService(MaxExportRows + 1)
	_, err := s.Export(context.Background(), owner, PaymentQuery{})
	assert.ErrorIs(t, err, ErrExportTooLarge)
	_, err = s.Export(context.Background(), reception, PaymentQuery{})
	assert.ErrorIs(t, err, ErrForbidden)

	s, owner, _ = newService(MaxExportRows)
	rows, err := s.Export(context.Background(), owner, PaymentQuery{})
	require.NoError(t, err)
	assert.Len(t, rows, MaxExportRows)
}

func TestRangeValidation(t *testing.T) {
	s, owner, _ := newService(1)
	day := func(m time.Month, d int) *time.Time { t := time.Date(2026, m, d, 0, 0, 0, 0, time.UTC); return &t }
	_, _, _, err := s.Payments(context.Background(), owner, PaymentQuery{From: day(10, 20), To: day(10, 1), Page: 1, PageSize: 10})
	assert.ErrorIs(t, err, ErrRange, "from after to")
	_, _, _, err = s.Payments(context.Background(), owner, PaymentQuery{From: func() *time.Time { t := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC); return &t }(), To: day(12, 31), Page: 1, PageSize: 10})
	assert.ErrorIs(t, err, ErrRange, "wider than 366 days")
	_, _, totals, err := s.Payments(context.Background(), owner, PaymentQuery{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.NotNil(t, totals)
}

func TestReceptionGetsNoTotalsAndNoDashboard(t *testing.T) {
	s, _, reception := newService(1)
	_, _, totals, err := s.Payments(context.Background(), reception, PaymentQuery{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Nil(t, totals)
	_, err = s.Dashboard(context.Background(), reception, nil, nil)
	assert.ErrorIs(t, err, ErrForbidden)
	other := uuid.New()
	_, err = s.Dashboard(context.Background(), reception, &other, nil)
	assert.Error(t, err)
}
