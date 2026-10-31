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

// StatusOf derives the status and the days remaining until the latest end date.
// With no periods the member is expired with zero days.
func StatusOf(periods []Period, today time.Time) (Status, int) {
	if len(periods) == 0 {
		return StatusExpired, 0
	}
	latest := periods[0].EndsOn
	active, scheduled := false, false
	for _, p := range periods {
		if p.EndsOn.After(latest) {
			latest = p.EndsOn
		}
		if !p.StartsOn.After(today) && !p.EndsOn.Before(today) {
			active = true
		}
		if p.StartsOn.After(today) {
			scheduled = true
		}
	}
	days := int(latest.Sub(today).Hours() / 24)
	switch {
	case active && days <= ExpiringDays:
		return StatusExpiring, days
	case active:
		return StatusActive, days
	case scheduled:
		return StatusScheduled, days
	default:
		return StatusExpired, days
	}
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
