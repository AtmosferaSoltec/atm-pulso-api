package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// Optional distinguishes absent fields from explicit null in partial updates.
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	return json.Unmarshal(data, &o.Value)
}

// GymChanges is what the admin of a gym may change: day pass price, branch sharing and membership
// prices. The display name and the colour are kept only to answer 403: the identity of a gym is
// configured by Atmósfera from the console, not by the gym.
type GymChanges struct {
	DisplayName   Optional[string] `json:"display_name"`
	BrandColor    Optional[string] `json:"brand_color"`
	DayPassPrice  Optional[string] `json:"day_pass_price"`
	BranchSharing Optional[bool]   `json:"branch_sharing"`
	// MembershipPrices replaces the whole price list of the gym.
	MembershipPrices Optional[[]MembershipPrice] `json:"membership_prices"`
}

type GymRepository interface {
	// UpdateGym changes the current gym (the service already checked it is the caller's).
	UpdateGym(ctx context.Context, gymID uuid.UUID, changes GymChanges) error
}

var colorPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)
var pricePattern = regexp.MustCompile(`^[0-9]{1,7}\.[0-9]{2}$`)

const maxMembershipMonths = 24

// normalizePrices accepts one price per duration (1 to 24 whole months) and returns the list in
// the order given: it is the order the gym wants them offered when selling. It reports false for a repeated or out-of-range duration or a bad amount.
func normalizePrices(prices []MembershipPrice) ([]MembershipPrice, bool) {
	if len(prices) > maxMembershipMonths {
		return nil, false
	}
	seen := make(map[int]bool, len(prices))
	for _, price := range prices {
		if price.Months < 1 || price.Months > maxMembershipMonths || seen[price.Months] || !pricePattern.MatchString(price.Amount) {
			return nil, false
		}
		cents, err := strconv.ParseInt(strings.ReplaceAll(price.Amount, ".", ""), 10, 64)
		if err != nil || cents > 100_000_000 {
			return nil, false
		}
		seen[price.Months] = true
	}
	return append([]MembershipPrice{}, prices...), true
}

// UpdateGym changes the rules of the admin's current gym: day pass price, branch sharing and
// membership prices. Its identity (name, RUC, colour) and branches are not the gym's to change.
func (s *Service) UpdateGym(ctx context.Context, p auth.Principal, id uuid.UUID, input GymChanges) (Gym, error) {
	if !p.CanManageGym(id) {
		if p.Role != auth.RoleAdmin {
			return Gym{}, domain.ErrForbidden
		}
		return Gym{}, domain.ErrScopeNotAllowed
	}
	if input.DisplayName.Set || input.BrandColor.Set {
		return Gym{}, domain.ErrForbidden
	}
	if input.BranchSharing.Set && input.BranchSharing.Value == nil {
		return Gym{}, fieldError("branch_sharing", "BOOLEAN_REQUIRED")
	}
	if input.DayPassPrice.Set && input.DayPassPrice.Value != nil {
		price := *input.DayPassPrice.Value
		if !pricePattern.MatchString(price) {
			return Gym{}, fieldError("day_pass_price", "PRICE_INVALID")
		}
		cents, err := strconv.ParseInt(strings.ReplaceAll(price, ".", ""), 10, 64)
		if err != nil || cents > 100_000_000 {
			return Gym{}, fieldError("day_pass_price", "PRICE_INVALID")
		}
	}
	if input.MembershipPrices.Set {
		if input.MembershipPrices.Value == nil {
			return Gym{}, fieldError("membership_prices", "PRICES_INVALID")
		}
		prices, ok := normalizePrices(*input.MembershipPrices.Value)
		if !ok {
			return Gym{}, fieldError("membership_prices", "PRICES_INVALID")
		}
		input.MembershipPrices.Value = &prices
	}
	if err := s.Gyms.UpdateGym(ctx, id, input); err != nil {
		return Gym{}, err
	}
	catalog, err := s.Catalog(ctx, p)
	if err != nil {
		return Gym{}, err
	}
	for _, gym := range catalog.Gyms {
		if gym.ID == id {
			return gym, nil
		}
	}
	return Gym{}, domain.ErrResourceNotFound
}

func fieldError(field, code string) error {
	return &domain.ValidationError{Issues: []domain.Issue{{Field: field, Code: code}}}
}
