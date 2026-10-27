package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestEndDate(t *testing.T) {
	cases := []struct {
		start  time.Time
		months int
		want   time.Time
	}{
		{day(2026, 1, 31), 1, day(2026, 2, 28)},
		{day(2028, 1, 31), 1, day(2028, 2, 29)},
		{day(2026, 10, 27), 3, day(2027, 1, 27)},
		{day(2026, 12, 15), 1, day(2027, 1, 15)},
		{day(2026, 8, 31), 6, day(2027, 2, 28)},
		{day(2026, 3, 1), 24, day(2028, 3, 1)},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, EndDate(c.start, c.months), "%v + %d", c.start, c.months)
	}
}

func TestStatusOf(t *testing.T) {
	today := day(2026, 10, 27)
	cases := []struct {
		name    string
		periods []Period
		status  Status
		days    int
	}{
		{"none", nil, StatusExpired, 0},
		{"ends today", []Period{{day(2026, 9, 27), today}}, StatusExpiring, 0},
		{"7 days left", []Period{{day(2026, 9, 1), day(2026, 11, 3)}}, StatusExpiring, 7},
		{"8 days left", []Period{{day(2026, 9, 1), day(2026, 11, 4)}}, StatusActive, 8},
		{"starts today", []Period{{today, day(2027, 1, 27)}}, StatusActive, 92},
		{"ended yesterday", []Period{{day(2026, 9, 1), day(2026, 10, 26)}}, StatusExpired, -1},
		{"future only", []Period{{day(2026, 10, 28), day(2027, 1, 28)}}, StatusScheduled, 93},
		{"expired and scheduled", []Period{{day(2026, 1, 1), day(2026, 2, 1)}, {day(2026, 11, 1), day(2026, 12, 1)}}, StatusScheduled, 35},
		{"active plus next", []Period{{day(2026, 10, 1), day(2026, 10, 30)}, {day(2026, 10, 31), day(2027, 1, 31)}}, StatusActive, 96},
	}
	for _, c := range cases {
		status, days := StatusOf(c.periods, today)
		assert.Equal(t, c.status, status, c.name)
		assert.Equal(t, c.days, days, c.name)
	}
}

func TestPlanLabel(t *testing.T) {
	assert.Equal(t, "1 mes", PlanLabel(1))
	assert.Equal(t, "3 meses", PlanLabel(3))
	assert.Equal(t, "24 meses", PlanLabel(24))
}

func TestDocumentsAndPhone(t *testing.T) {
	assert.True(t, ValidDocument("dni", "12345678"))
	assert.False(t, ValidDocument("dni", "1234567"))
	assert.False(t, ValidDocument("dni", "1234567a"))
	assert.True(t, ValidDocument("ce", "123456789"))
	assert.True(t, ValidDocument("ce", "123456789012"))
	assert.False(t, ValidDocument("ce", "12345678"))
	assert.False(t, ValidDocument("pasaporte", "12345678"))
	assert.True(t, ValidPhone("999111222"))
	assert.False(t, ValidPhone("899111222"))
	assert.False(t, ValidPhone("99911122"))
	name, ok := NormalizeName("  Ana   María  Pérez ")
	assert.True(t, ok)
	assert.Equal(t, "Ana María Pérez", name)
	_, ok = NormalizeName("Al")
	assert.False(t, ok)
}

func TestAccessBranches(t *testing.T) {
	gymA, gymB := uuid.New(), uuid.New()
	a1, a2, b1 := Branch{uuid.New(), gymA}, Branch{uuid.New(), gymA}, Branch{uuid.New(), gymB}
	all := []Branch{a1, a2, b1}

	got, err := AccessBranches(ScopeBranch, a1, all, nil)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a1.ID}, got)

	got, err = AccessBranches(ScopeGym, a1, all, nil)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a1.ID, a2.ID}, got)

	got, err = AccessBranches(ScopeNetwork, a1, all, nil)
	require.NoError(t, err)
	assert.Len(t, got, 3)

	got, err = AccessBranches(ScopeNetwork, a1, all, []uuid.UUID{a1.ID, b1.ID, b1.ID})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a1.ID, b1.ID}, got)

	_, err = AccessBranches(ScopeGym, a1, all, []uuid.UUID{a1.ID, b1.ID})
	assert.ErrorIs(t, err, ErrAccessBranches, "outside the scope")
	_, err = AccessBranches(ScopeNetwork, a1, all, []uuid.UUID{a2.ID})
	assert.ErrorIs(t, err, ErrAccessBranches, "home branch missing")
	_, err = AccessBranches(ScopeNetwork, a1, all, []uuid.UUID{a1.ID, uuid.New()})
	assert.ErrorIs(t, err, ErrAccessBranches, "foreign branch")
}
