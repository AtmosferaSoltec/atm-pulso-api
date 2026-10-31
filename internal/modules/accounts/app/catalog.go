package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// Catalog is the current gym with its branches (the only entry of Gyms); Accessible says which
// branches the person may operate. Other gyms, even of the same group, are never listed.
type Catalog struct {
	Gyms []Gym `json:"gyms"`
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
	// LogoURL is a temporary signed address of the logo in the company profile; null when none.
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

// Catalog gives every role the gym's directory while deriving access from the live principal.
func (s *Service) Catalog(ctx context.Context, principal auth.Principal) (Catalog, error) {
	catalog, err := s.Catalogs.Catalog(ctx, principal.GymID)
	if err != nil {
		return Catalog{}, err
	}
	for gi := range catalog.Gyms {
		gym := &catalog.Gyms[gi]
		gym.LogoURL = s.logoOf(ctx, principal)
		for bi := range gym.Branches {
			branch := &gym.Branches[bi]
			branch.Accessible = principal.CanAccessBranch(auth.BranchRef{ID: branch.ID, GymID: gym.ID})
		}
	}
	return catalog, nil
}
