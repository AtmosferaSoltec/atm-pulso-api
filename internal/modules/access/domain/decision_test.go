package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestDecide(t *testing.T) {
	today := day(2026, 10, 27)
	home, other, far := uuid.New(), uuid.New(), uuid.New()
	period := func(start, end time.Time, branches ...uuid.UUID) []Period {
		return []Period{{StartsOn: start, EndsOn: end, BranchIDs: branches}}
	}
	cases := []struct {
		name     string
		periods  []Period
		branch   uuid.UUID
		sharing  bool
		allowed  bool
		reason   string
		expiring *int
	}{
		{"valid", period(day(2026, 10, 1), day(2026, 12, 1), home), home, true, true, ReasonValid, nil},
		{"ends today", period(day(2026, 9, 27), today, home), home, true, true, ReasonValid, ptr(0)},
		{"7 days left", period(day(2026, 9, 1), day(2026, 11, 3), home), home, true, true, ReasonValid, ptr(7)},
		{"8 days left", period(day(2026, 9, 1), day(2026, 11, 4), home), home, true, true, ReasonValid, nil},
		{"starts today", period(today, day(2026, 12, 1), home), home, true, true, ReasonValid, nil},
		{"ended yesterday", period(day(2026, 9, 1), day(2026, 10, 26), home), home, true, false, ReasonExpired, nil},
		{"not started", period(day(2026, 10, 28), day(2026, 12, 1), home), home, true, false, ReasonNotStarted, nil},
		{"no periods", nil, home, true, false, ReasonExpired, nil},
		{"branch not in the list", period(day(2026, 10, 1), day(2026, 12, 1), home), other, true, false, ReasonBranch, nil},
		{"subset includes other", period(day(2026, 10, 1), day(2026, 12, 1), home, other), other, true, true, ReasonValid, nil},
		{"sharing off blocks other branch", period(day(2026, 10, 1), day(2026, 12, 1), home, other), other, false, false, ReasonBranch, nil},
		{"sharing off allows home", period(day(2026, 10, 1), day(2026, 12, 1), home, other), home, false, true, ReasonValid, nil},
		{"far branch denied", period(day(2026, 10, 1), day(2026, 12, 1), home), far, true, false, ReasonBranch, nil},
	}
	for _, c := range cases {
		d := Decide(c.periods, c.branch, home, c.sharing, today)
		assert.Equal(t, c.allowed, d.Allowed, c.name)
		assert.Equal(t, c.reason, d.Reason, c.name)
		assert.Equal(t, c.expiring, d.ExpiringInDays, c.name)
	}
}

func ptr(n int) *int { return &n }

// A member with several current periods (one per plan): any of them that opens the branch lets them in.
func TestDecideSeveralPlans(t *testing.T) {
	today := day(2026, 10, 27)
	a, b := uuid.New(), uuid.New()
	baile := Period{StartsOn: day(2026, 10, 1), EndsOn: day(2026, 10, 30), BranchIDs: []uuid.UUID{a}}
	todo := Period{StartsOn: day(2026, 10, 1), EndsOn: day(2027, 1, 1), BranchIDs: []uuid.UUID{a, b}}

	d := Decide([]Period{baile, todo}, b, a, true, today)
	assert.True(t, d.Allowed, "the plan that opens branch B lets them in")
	assert.Equal(t, todo.EndsOn, *d.EndsOn)
	assert.Nil(t, d.ExpiringInDays)

	d = Decide([]Period{baile, todo}, a, a, true, today)
	assert.True(t, d.Allowed)
	assert.Equal(t, todo.EndsOn, *d.EndsOn, "the later end among the plans that open the branch")

	d = Decide([]Period{baile, {StartsOn: day(2026, 8, 1), EndsOn: day(2026, 9, 1), BranchIDs: []uuid.UUID{a, b}}}, b, a, true, today)
	assert.False(t, d.Allowed, "the only current plan does not open branch B, the expired one does not count")
	assert.Equal(t, ReasonBranch, d.Reason)

	d = Decide([]Period{baile, {StartsOn: day(2026, 10, 30), EndsOn: day(2026, 12, 30), BranchIDs: []uuid.UUID{a}}}, a, a, true, today)
	assert.True(t, d.Allowed)
	assert.Equal(t, day(2026, 12, 30), *d.EndsOn, "a queued period continues the access")
	assert.Nil(t, d.ExpiringInDays)

	d = Decide([]Period{baile}, a, a, true, today)
	require.NotNil(t, d.ExpiringInDays)
	assert.Equal(t, 3, *d.ExpiringInDays)
}
