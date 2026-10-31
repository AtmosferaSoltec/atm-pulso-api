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

// GymChanges is what the admin of a gym may change: day pass price and branch sharing (membership prices
// belong to the plans of the members module). The display name and the colour are kept only to answer 403: the identity of a gym is
// configured by Atmósfera from the console, not by the gym.
type GymChanges struct {
	DisplayName   Optional[string] `json:"display_name"`
	BrandColor    Optional[string] `json:"brand_color"`
	DayPassPrice  Optional[string] `json:"day_pass_price"`
	BranchSharing Optional[bool]   `json:"branch_sharing"`
}

type GymRepository interface {
	// UpdateGym changes the current gym (the service already checked it is the caller's).
	UpdateGym(ctx context.Context, gymID uuid.UUID, changes GymChanges) error
}

var colorPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)
var pricePattern = regexp.MustCompile(`^[0-9]{1,7}\.[0-9]{2}$`)

// UpdateGym changes the rules of the admin's current gym: day pass price and branch sharing. Its identity (name, RUC, colour) and branches are not the gym's to change.
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
