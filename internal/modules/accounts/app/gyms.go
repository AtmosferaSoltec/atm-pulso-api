package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

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

type GymChanges struct {
	DisplayName   Optional[string] `json:"display_name"`
	BrandColor    Optional[string] `json:"brand_color"`
	DayPassPrice  Optional[string] `json:"day_pass_price"`
	BranchSharing Optional[bool]   `json:"branch_sharing"`
	// MembershipPrices replaces the whole price list of the gym.
	MembershipPrices Optional[[]MembershipPrice] `json:"membership_prices"`
}

type GymRepository interface {
	UpdateGym(context.Context, uuid.UUID, uuid.UUID, GymChanges) error
	// GymOrganization returns the organization a gym belongs to (domain.ErrResourceNotFound if none).
	GymOrganization(ctx context.Context, gymID uuid.UUID) (uuid.UUID, error)
	// SetLogoKey stores (or clears, with nil) the logo and returns the key it replaced.
	// LogoKey is the stored logo of a gym, nil when it has none (domain.ErrResourceNotFound if no gym).
	LogoKey(ctx context.Context, gymID uuid.UUID) (*string, error)
	SetLogoKey(ctx context.Context, organizationID, gymID uuid.UUID, key *string) (*string, error)
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

// UpdateGym changes a gym. The identity (display name, colour, logo) is set only by the
// superadmin, who may change nothing else; owners and admins change everything but the identity.
func (s *Service) UpdateGym(ctx context.Context, p auth.Principal, id uuid.UUID, input GymChanges) (Gym, error) {
	identityChange := input.DisplayName.Set || input.BrandColor.Set
	switch p.Role {
	case auth.RoleSuperadmin:
		if input.DayPassPrice.Set || input.BranchSharing.Set || input.MembershipPrices.Set {
			return Gym{}, domain.ErrForbidden
		}
		org, err := s.Gyms.GymOrganization(ctx, id)
		if err != nil {
			return Gym{}, err
		}
		p.OrganizationID = org
	case auth.RoleOwner, auth.RoleAdmin:
		if identityChange {
			return Gym{}, domain.ErrForbidden
		}
	default:
		return Gym{}, domain.ErrForbidden
	}
	catalog, err := s.Catalogs.Catalog(ctx, p.OrganizationID)
	if err != nil {
		return Gym{}, err
	}
	found := false
	for _, gym := range catalog.Gyms {
		if gym.ID == id {
			found = true
			break
		}
	}
	if !found {
		return Gym{}, domain.ErrResourceNotFound
	}
	if p.Role != auth.RoleSuperadmin && !p.CanManageGym(id) {
		return Gym{}, domain.ErrScopeNotAllowed
	}
	if input.DisplayName.Set && input.DisplayName.Value != nil {
		name := strings.TrimSpace(*input.DisplayName.Value)
		if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 80 {
			return Gym{}, fieldError("display_name", "NAME_REQUIRED")
		}
		input.DisplayName.Value = &name
	}
	if input.BrandColor.Set && (input.BrandColor.Value == nil || !colorPattern.MatchString(*input.BrandColor.Value)) {
		return Gym{}, fieldError("brand_color", "BRAND_COLOR_INVALID")
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
	if err := s.Gyms.UpdateGym(ctx, p.OrganizationID, id, input); err != nil {
		return Gym{}, err
	}
	catalog, err = s.Catalog(ctx, p)
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
