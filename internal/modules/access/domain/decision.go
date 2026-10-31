// Package domain holds the pure access rules: whether a member may enter a branch today.
package domain

import (
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Reason codes stored with every access entry (the web writes the Spanish text).
const (
	ReasonValid       = "MEMBERSHIP_VALID"
	ReasonExpired     = "MEMBERSHIP_EXPIRED"
	ReasonNotStarted  = "MEMBERSHIP_NOT_STARTED"
	ReasonBranch      = "BRANCH_NOT_ALLOWED"
	ReasonNotRegister = "NOT_REGISTERED"
	ReasonDayPass     = "DAY_PASS"
)

// ExpiringDays: an allowed entry carries a warning when the membership ends in this many days or fewer.
const ExpiringDays = 7

// Period is one sold membership with the branches it opens.
type Period struct {
	StartsOn  time.Time
	EndsOn    time.Time
	BranchIDs []uuid.UUID
}

// Decision is the answer to an access check.
type Decision struct {
	Allowed bool
	Reason  string
	// EndsOn is the end date to show: the current period when allowed or branch denied,
	// the last end date when expired, the start date when not started.
	EndsOn         *time.Time
	ExpiringInDays *int
}

// Decide applies business-rules section 11 to a registered member. homeBranch is the branch
// where the member enrolled; when its gym has branch sharing off, only that branch is allowed.
func Decide(periods []Period, branchID, homeBranch uuid.UUID, homeGymSharing bool, today time.Time) Decision {
	var active []Period
	var latest time.Time
	var nextStart *time.Time
	for _, p := range periods {
		if p.EndsOn.After(latest) {
			latest = p.EndsOn
		}
		switch {
		case !p.StartsOn.After(today) && !p.EndsOn.Before(today):
			active = append(active, p)
		case p.StartsOn.After(today):
			if nextStart == nil || p.StartsOn.Before(*nextStart) {
				start := p.StartsOn
				nextStart = &start
			}
		}
	}
	if len(active) == 0 {
		if nextStart != nil {
			return Decision{Reason: ReasonNotStarted, EndsOn: nextStart}
		}
		if len(periods) == 0 {
			return Decision{Reason: ReasonExpired}
		}
		return Decision{Reason: ReasonExpired, EndsOn: &latest}
	}
	if !homeGymSharing && branchID != homeBranch {
		return Decision{Reason: ReasonBranch}
	}
	// A member may hold several periods at once (one per plan). Entry is allowed when ANY current period,
	// of any plan, opens this branch: a plan does not restrict access by itself (plan 08).
	var opening []Period
	for _, p := range periods {
		if slices.Contains(p.BranchIDs, branchID) {
			opening = append(opening, p)
		}
	}
	for _, p := range active {
		if slices.Contains(p.BranchIDs, branchID) {
			// The end shown is where access to this branch stops: the latest end among the current periods
			// that open it, extended by any such period that continues it (same rule as the member status).
			end := coverageEnd(opening, today)
			d := Decision{Allowed: true, Reason: ReasonValid, EndsOn: &end}
			if days := int(end.Sub(today).Hours() / 24); days <= ExpiringDays {
				d.ExpiringInDays = &days
			}
			return d
		}
	}
	return Decision{Reason: ReasonBranch}
}

// coverageEnd extends the latest end among the current periods with the periods that begin on or before
// the day after it. At least one period must be current.
func coverageEnd(periods []Period, today time.Time) time.Time {
	var end time.Time
	for _, p := range periods {
		if !p.StartsOn.After(today) && !p.EndsOn.Before(today) && p.EndsOn.After(end) {
			end = p.EndsOn
		}
	}
	for extended := true; extended; {
		extended = false
		for _, p := range periods {
			if !p.StartsOn.After(end.AddDate(0, 0, 1)) && p.EndsOn.After(end) {
				end, extended = p.EndsOn, true
			}
		}
	}
	return end
}

var (
	// ErrMemberNotFound: the member id does not exist in the gym.
	ErrMemberNotFound = errors.New("member not found")
	// ErrBranchNotAccessible: the user cannot work in that branch.
	ErrBranchNotAccessible = errors.New("branch not accessible")
	// ErrForbidden: the role cannot do this.
	ErrForbidden = errors.New("forbidden")
	// ErrNotFound: day pass or entry not found.
	ErrNotFound = errors.New("not found")
	// ErrConflict: the id sent already belongs to something else.
	ErrConflict = errors.New("conflict")
)

// Issue is one problem found in a field.
type Issue struct{ Field, Code string }

// ValidationError lists the problems of the submitted fields.
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string { return "validation failed" }
