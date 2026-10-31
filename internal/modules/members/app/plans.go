package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

// PlanPrice is the suggested price of a plan for a duration in whole months.
type PlanPrice struct {
	Months int    `json:"months"`
	Amount string `json:"amount"`
}

// Plan is a membership plan of a gym (for instance "Baile" or "Todo el gym") with its price per duration.
type Plan struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	IsActive    bool        `json:"is_active"`
	Position    int         `json:"position"`
	Prices      []PlanPrice `json:"prices"`
}

// Optional distinguishes an absent field from an explicit null in a partial update.
type Optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON marks the field as present.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	return json.Unmarshal(data, &o.Value)
}

// PlanInput is the body of POST /membership-plans.
type PlanInput struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	IsActive    *bool       `json:"is_active"`
	Position    *int        `json:"position"`
	Prices      []PlanPrice `json:"prices"`
}

// PlanChanges is the body of PATCH /membership-plans/{id}: only the fields sent change.
// Prices replaces the whole price list.
type PlanChanges struct {
	Name        Optional[string]      `json:"name"`
	Description Optional[string]      `json:"description"`
	IsActive    Optional[bool]        `json:"is_active"`
	Position    Optional[int]         `json:"position"`
	Prices      Optional[[]PlanPrice] `json:"prices"`
}

const (
	maxPlanName        = 60
	maxPlanDescription = 200
	maxPlans           = 50
)

var priceAmountPattern = regexp.MustCompile(`^[0-9]{1,7}\.[0-9]{2}$`)

func planName(raw string) (string, bool) {
	name := strings.Join(strings.Fields(raw), " ")
	n := utf8.RuneCountInString(name)
	return name, n >= 1 && n <= maxPlanName
}

func planDescription(raw *string) (*string, bool) {
	if raw == nil {
		return nil, true
	}
	text := strings.Join(strings.Fields(*raw), " ")
	if text == "" {
		return nil, true
	}
	return &text, utf8.RuneCountInString(text) <= maxPlanDescription
}

// normalizePrices accepts one price per duration (1 to 24 whole months, "270.00") and returns them by months.
func normalizePrices(prices []PlanPrice) ([]PlanPrice, bool) {
	if len(prices) > 24 {
		return nil, false
	}
	seen := make(map[int]bool, len(prices))
	for _, price := range prices {
		if price.Months < 1 || price.Months > 24 || seen[price.Months] || !priceAmountPattern.MatchString(price.Amount) {
			return nil, false
		}
		cents, err := strconv.ParseInt(strings.ReplaceAll(price.Amount, ".", ""), 10, 64)
		if err != nil || cents > 100_000_000 {
			return nil, false
		}
		seen[price.Months] = true
	}
	out := append([]PlanPrice{}, prices...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Months < out[j-1].Months; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, true
}

// Plans lists the gym's plans: the admin sees all of them, the receptionist only the active ones
// (the ones that can be sold).
func (s *Service) Plans(ctx context.Context, p auth.Principal) ([]Plan, error) {
	return s.Repo.Plans(ctx, p.GymID, p.Role != auth.RoleAdmin)
}

// CreatePlan adds a plan to the admin's gym.
func (s *Service) CreatePlan(ctx context.Context, p auth.Principal, in PlanInput) (Plan, error) {
	if p.Role != auth.RoleAdmin {
		return Plan{}, domain.ErrForbidden
	}
	var issues []domain.Issue
	name, ok := planName(in.Name)
	if !ok {
		issues = append(issues, domain.Issue{Field: "name", Code: domain.CodePlanName})
	}
	description, ok := planDescription(in.Description)
	if !ok {
		issues = append(issues, domain.Issue{Field: "description", Code: domain.CodePlanDescription})
	}
	prices, ok := normalizePrices(in.Prices)
	if !ok {
		issues = append(issues, domain.Issue{Field: "prices", Code: domain.CodePlanPrices})
	}
	if len(issues) > 0 {
		return Plan{}, &domain.ValidationError{Issues: issues}
	}
	plan := Plan{ID: in.ID, Name: name, Description: description, IsActive: true, Prices: prices}
	if plan.ID == uuid.Nil {
		plan.ID = ids.New()
	}
	if in.IsActive != nil {
		plan.IsActive = *in.IsActive
	}
	position := -1
	if in.Position != nil {
		position = *in.Position
		if position < 0 || position > 1000 {
			return Plan{}, &domain.ValidationError{Issues: []domain.Issue{{Field: "position", Code: domain.CodePlanPosition}}}
		}
	}
	plan.Position = position
	return s.Repo.CreatePlan(ctx, p.GymID, plan)
}

// UpdatePlan edits a plan of the admin's gym. A plan is never deleted (its name stays on old receipts):
// it is deactivated, and then it can no longer be sold.
func (s *Service) UpdatePlan(ctx context.Context, p auth.Principal, id uuid.UUID, changes PlanChanges) (Plan, error) {
	if p.Role != auth.RoleAdmin {
		return Plan{}, domain.ErrForbidden
	}
	var issues []domain.Issue
	if changes.Name.Set {
		name, ok := "", false
		if changes.Name.Value != nil {
			name, ok = planName(*changes.Name.Value)
		}
		if !ok {
			issues = append(issues, domain.Issue{Field: "name", Code: domain.CodePlanName})
		}
		changes.Name.Value = &name
	}
	if changes.Description.Set {
		description, ok := planDescription(changes.Description.Value)
		if !ok {
			issues = append(issues, domain.Issue{Field: "description", Code: domain.CodePlanDescription})
		}
		changes.Description.Value = description
	}
	if changes.IsActive.Set && changes.IsActive.Value == nil {
		issues = append(issues, domain.Issue{Field: "is_active", Code: domain.CodePlanActive})
	}
	if changes.Position.Set && (changes.Position.Value == nil || *changes.Position.Value < 0 || *changes.Position.Value > 1000) {
		issues = append(issues, domain.Issue{Field: "position", Code: domain.CodePlanPosition})
	}
	if changes.Prices.Set {
		var prices []PlanPrice
		ok := false
		if changes.Prices.Value != nil {
			prices, ok = normalizePrices(*changes.Prices.Value)
		}
		if !ok {
			issues = append(issues, domain.Issue{Field: "prices", Code: domain.CodePlanPrices})
		}
		changes.Prices.Value = &prices
	}
	if len(issues) > 0 {
		return Plan{}, &domain.ValidationError{Issues: issues}
	}
	return s.Repo.UpdatePlan(ctx, p.GymID, id, changes)
}
