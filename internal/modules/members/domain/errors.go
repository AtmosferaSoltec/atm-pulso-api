package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrMemberNotFound: the member does not exist in the gym.
	ErrMemberNotFound = errors.New("member not found")
	// ErrReceiptNotFound: the receipt does not exist in the gym.
	ErrReceiptNotFound = errors.New("receipt not found")
	// ErrBranchNotAccessible: the user cannot work in that branch.
	ErrBranchNotAccessible = errors.New("branch not accessible")
	// ErrForbidden: the role cannot do this.
	ErrForbidden = errors.New("forbidden")
	// ErrPlanNotFound: the membership plan does not exist in the gym.
	ErrPlanNotFound = errors.New("plan not found")
	// ErrPlanNameTaken: the gym already has a plan with that name.
	ErrPlanNameTaken = errors.New("plan name taken")
	// ErrMembershipNotFound: the period does not exist for that member in the gym.
	ErrMembershipNotFound = errors.New("membership not found")
	// ErrMembershipEnded: the period already ended (before today) and can no longer be changed.
	ErrMembershipEnded = errors.New("membership ended")
)

// OverlapError: the period would overlap another period of the same plan of the same member. It carries the
// period in the way and the day a period of that plan can start (where the last one ends).
type OverlapError struct {
	MembershipID   uuid.UUID
	StartsOn       time.Time
	EndsOn         time.Time
	SuggestedStart time.Time
}

func (e *OverlapError) Error() string { return "membership overlaps another period of the same plan" }

// DocumentTakenError carries the member that already uses the document.
type DocumentTakenError struct{ MemberID uuid.UUID }

func (e *DocumentTakenError) Error() string { return "member document already registered" }

// Issue is one problem found in a field.
type Issue struct{ Field, Code string }

// ValidationError lists the problems of the submitted fields.
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string { return "validation failed" }

// Field codes; the web picks its own Spanish text.
const (
	CodeDocumentType   = "DOCUMENT_TYPE_INVALID"
	CodeDocumentNumber = "DOCUMENT_NUMBER_INVALID"
	CodeFullName       = "FULL_NAME_INVALID"
	CodePhone          = "PHONE_INVALID"
	CodeMonths         = "MONTHS_INVALID"
	CodeStartDate      = "START_DATE_INVALID"
	CodeAmount         = "AMOUNT_INVALID"
	CodeMethod         = "METHOD_INVALID"
	CodeAccessScope    = "ACCESS_SCOPE_INVALID"
	// CodePlanRequired: the gym has several active plans (or none) and the sale did not say which.
	CodePlanRequired = "PLAN_REQUIRED"
	// CodePlanInvalid: the plan is not an active plan of the gym.
	CodePlanInvalid     = "PLAN_INVALID"
	CodePlanName        = "PLAN_NAME_INVALID"
	CodePlanDescription = "PLAN_DESCRIPTION_INVALID"
	CodePlanPrices      = "PLAN_PRICES_INVALID"
	CodePlanActive      = "PLAN_ACTIVE_INVALID"
	CodePlanPosition    = "PLAN_POSITION_INVALID"
	// CodePeriodEnded: the corrected period would end before today.
	CodePeriodEnded = "PERIOD_ENDS_IN_PAST"
	// CodeNoChanges: a correction without any field.
	CodeNoChanges = "NO_CHANGES"
)

// ErrConflict: the id sent by the client already belongs to something else.
var ErrConflict = errors.New("conflict")
