// Package domain holds the pure rules of members and memberships: dates, status,
// documents and which branches a membership opens. No I/O.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Status of a member on a given day.
type Status string

const (
	StatusActive    Status = "active"
	StatusExpiring  Status = "expiring"
	StatusExpired   Status = "expired"
	StatusScheduled Status = "scheduled"
)

// ExpiringDays: a membership ending in this many days or fewer is "expiring".
const ExpiringDays = 7

// Period is one sold membership.
type Period struct {
	StartsOn time.Time
	EndsOn   time.Time
}

// Scope says which branches a membership opens by default.
type Scope string

const (
	ScopeBranch Scope = "branch"
	ScopeGym    Scope = "gym"
)

// Valid reports whether the scope is one of the two known values. There is no wider scope:
// nothing is shared between gyms (plan 07).
func (s Scope) Valid() bool { return s == ScopeBranch || s == ScopeGym }

// Branch is a branch and its gym.
type Branch struct {
	ID    uuid.UUID
	GymID uuid.UUID
}

// EndDate is start plus whole calendar months, clamped to the last day of the target
// month (Jan 31 + 1 month = Feb 28/29). Dates are midnight UTC.
func EndDate(start time.Time, months int) time.Time {
	lastOfTarget := time.Date(start.Year(), start.Month()+time.Month(months)+1, 0, 0, 0, 0, 0, time.UTC)
	day := start.Day()
	if day > lastOfTarget.Day() {
		day = lastOfTarget.Day()
	}
	return time.Date(lastOfTarget.Year(), lastOfTarget.Month(), day, 0, 0, 0, 0, time.UTC)
}

// Overlaps says whether two periods of the same plan collide. Periods are half-open, [starts_on, ends_on):
// the end date is the day the next period of the plan may begin, so a renewal that starts on the end date
// of the previous period does NOT overlap it, while starting one day earlier does.
func Overlaps(a, b Period) bool {
	return a.StartsOn.Before(b.EndsOn) && b.StartsOn.Before(a.EndsOn)
}

// Ended says whether a period is over: its last day (ends_on, inclusive for coverage) is before today.
func Ended(p Period, today time.Time) bool { return p.EndsOn.Before(today) }

// CoverageEnd is where a member's coverage stops, or nil when no period is current on today. Periods
// may belong to different plans and overlap. Start from the latest end among the periods that are
// current (starts_on <= today <= ends_on, both inclusive) and keep extending it with any period that
// begins on or before the day after it, so a renewal queued behind the current period (or another plan
// that continues it) counts. The same rule is written in SQL in platform/memberstatus.
func CoverageEnd(periods []Period, today time.Time) *time.Time {
	var end *time.Time
	for _, p := range periods {
		if !p.StartsOn.After(today) && !p.EndsOn.Before(today) && (end == nil || p.EndsOn.After(*end)) {
			e := p.EndsOn
			end = &e
		}
	}
	if end == nil {
		return nil
	}
	for extended := true; extended; {
		extended = false
		for _, p := range periods {
			if !p.StartsOn.After(end.AddDate(0, 0, 1)) && p.EndsOn.After(*end) {
				e := p.EndsOn
				end, extended = &e, true
			}
		}
	}
	return end
}

// StatusOf derives the status of a member from all their periods, whatever their plan, and the days
// remaining. Covered (some period current today): "expiring" when the coverage end (see CoverageEnd) is
// ExpiringDays or fewer away, else "active"; days = coverage end - today. Not covered: "scheduled" when a
// period starts later, else "expired"; days = latest end of any period - today (negative when past).
// With no periods the member is expired with zero days.
func StatusOf(periods []Period, today time.Time) (Status, int) {
	if len(periods) == 0 {
		return StatusExpired, 0
	}
	if end := CoverageEnd(periods, today); end != nil {
		days := int(end.Sub(today).Hours() / 24)
		if days <= ExpiringDays {
			return StatusExpiring, days
		}
		return StatusActive, days
	}
	latest := periods[0].EndsOn
	scheduled := false
	for _, p := range periods {
		if p.EndsOn.After(latest) {
			latest = p.EndsOn
		}
		if p.StartsOn.After(today) {
			scheduled = true
		}
	}
	days := int(latest.Sub(today).Hours() / 24)
	if scheduled {
		return StatusScheduled, days
	}
	return StatusExpired, days
}

// PlanLabel is "1 mes" or "N meses".
func PlanLabel(months int) string {
	if months == 1 {
		return "1 mes"
	}
	return itoa(months) + " meses"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

var (
	dniPattern   = regexp.MustCompile(`^[0-9]{8}$`)
	cePattern    = regexp.MustCompile(`^[0-9]{9,12}$`)
	phonePattern = regexp.MustCompile(`^9[0-9]{8}$`)
)

// ValidDocument checks a number against its type: dni has 8 digits, ce 9 to 12.
func ValidDocument(documentType, number string) bool {
	switch documentType {
	case "dni":
		return dniPattern.MatchString(number)
	case "ce":
		return cePattern.MatchString(number)
	default:
		return false
	}
}

// ValidPhone: nine digits starting with 9.
func ValidPhone(phone string) bool { return phonePattern.MatchString(phone) }

// NormalizeName trims and collapses spaces. A valid name has 3 to 120 characters.
func NormalizeName(name string) (string, bool) {
	name = strings.Join(strings.Fields(name), " ")
	n := utf8.RuneCountInString(name)
	return name, n >= 3 && n <= 120
}

// ErrAccessBranches: the requested branches are not a valid subset.
var ErrAccessBranches = errors.New("access branches invalid")

// AccessBranches returns the branches a membership opens. Without a request it is every
// branch of the scope; with one, the request must be a subset of the scope and contain
// the home branch. all lists every branch of the gym.
func AccessBranches(scope Scope, home Branch, all []Branch, requested []uuid.UUID) ([]uuid.UUID, error) {
	inScope := make([]uuid.UUID, 0, len(all))
	for _, b := range all {
		switch scope {
		case ScopeBranch:
			if b.ID == home.ID {
				inScope = append(inScope, b.ID)
			}
		case ScopeGym:
			if b.GymID == home.GymID {
				inScope = append(inScope, b.ID)
			}
		}
	}
	if len(requested) == 0 {
		return inScope, nil
	}
	allowed := make(map[uuid.UUID]bool, len(inScope))
	for _, id := range inScope {
		allowed[id] = true
	}
	seen := make(map[uuid.UUID]bool, len(requested))
	out := make([]uuid.UUID, 0, len(requested))
	for _, id := range requested {
		if !allowed[id] {
			return nil, ErrAccessBranches
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if !seen[home.ID] {
		return nil, ErrAccessBranches
	}
	return out, nil
}
