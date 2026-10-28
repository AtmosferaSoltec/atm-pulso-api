package app

import (
	"context"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/google/uuid"
)

// Catalog is the complete directory; Accessible controls which branches may be operated.
type Catalog struct {
	Organization CatalogName `json:"organization"`
	Gyms         []Gym       `json:"gyms"`
}

type CatalogName struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type Gym struct {
	ID                    uuid.UUID `json:"id"`
	Name                  string    `json:"name"`
	DisplayName           *string   `json:"display_name"`
	TaxID                 *string   `json:"tax_id"`
	BrandColor            string    `json:"brand_color"`
	DayPassPrice          string    `json:"day_pass_price"`
	DayPassPriceIsDefault bool      `json:"day_pass_price_is_default"`
	BranchSharing         bool      `json:"branch_sharing"`
	// LogoKey is where the logo lives in the object store; clients get LogoURL instead.
	LogoKey *string `json:"-"`
	LogoURL *string `json:"logo_url"`
	// MembershipPrices is the gym's base price per duration, ordered by months.
	MembershipPrices []MembershipPrice `json:"membership_prices"`
	Branches         []Branch          `json:"branches"`
}

// MembershipPrice is the base price of a membership of the given whole months.
type MembershipPrice struct {
	Months int    `json:"months"`
	Amount string `json:"amount"`
}

type Branch struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Address    *string   `json:"address"`
	Accessible bool      `json:"accessible"`
}

type CatalogRepository interface {
	Catalog(context.Context, uuid.UUID) (Catalog, error)
}

// Catalog gives every role the shared directory while deriving access from the live principal.
func (s *Service) Catalog(ctx context.Context, principal auth.Principal) (Catalog, error) {
	catalog, err := s.Catalogs.Catalog(ctx, principal.OrganizationID)
	if err != nil {
		return Catalog{}, err
	}
	for gi := range catalog.Gyms {
		gym := &catalog.Gyms[gi]
		gym.LogoURL = s.logoURL(gym.ID, gym.LogoKey)
		for bi := range gym.Branches {
			branch := &gym.Branches[bi]
			branch.Accessible = principal.CanAccessBranch(auth.BranchRef{ID: branch.ID, GymID: gym.ID})
		}
	}
	return catalog, nil
}
