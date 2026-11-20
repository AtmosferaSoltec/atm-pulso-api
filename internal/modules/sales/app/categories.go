package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// ---- Product categories ----
// The gym's admin manages them from the web; the platform superadmin does the same from console-web for any gym
// (the Admin* methods, which take the gym from the URL and are reached only behind the superadmin guard).

// Categories lists the categories of the person's gym (any staff role).
func (s *Service) Categories(ctx context.Context, p auth.Principal) ([]CategoryView, error) {
	list, err := s.Repo.Categories(ctx, p.GymID)
	if err != nil {
		return nil, err
	}
	out := make([]CategoryView, 0, len(list))
	for _, c := range list {
		out = append(out, c.CategoryView)
	}
	return out, nil
}

// CreateCategory adds one category at the end of the list (admin). A name the gym already has is 409.
func (s *Service) CreateCategory(ctx context.Context, p auth.Principal, in CategoryInput) (CategoryView, error) {
	if err := requireAdmin(p); err != nil {
		return CategoryView{}, err
	}
	name, ok := domain.CategoryName(in.Name)
	if !ok {
		return CategoryView{}, invalid("name", domain.CodeCategoryName)
	}
	created, _, err := s.Repo.CreateCategories(ctx, p.GymID, []string{name})
	if err != nil {
		return CategoryView{}, err
	}
	if len(created) == 0 {
		return CategoryView{}, domain.ErrCategoryNameTaken
	}
	return created[0], nil
}

// UpdateCategory renames or moves a category (admin).
func (s *Service) UpdateCategory(ctx context.Context, p auth.Principal, id uuid.UUID, in CategoryChanges) (CategoryView, error) {
	if err := requireAdmin(p); err != nil {
		return CategoryView{}, err
	}
	return s.updateCategory(ctx, p.GymID, id, in)
}

// DeleteCategory removes a category (admin); its products stay, without category.
func (s *Service) DeleteCategory(ctx context.Context, p auth.Principal, id uuid.UUID) error {
	if err := requireAdmin(p); err != nil {
		return err
	}
	return s.Repo.DeleteCategory(ctx, p.GymID, id)
}

func (s *Service) updateCategory(ctx context.Context, gymID, id uuid.UUID, in CategoryChanges) (CategoryView, error) {
	var issues []domain.Issue
	var name *string
	if in.Name != nil {
		normalized, ok := domain.CategoryName(*in.Name)
		if !ok {
			issues = append(issues, domain.Issue{Field: "name", Code: domain.CodeCategoryName})
		}
		name = &normalized
	}
	if in.Position != nil && (*in.Position < 0 || *in.Position > 10_000) {
		issues = append(issues, domain.Issue{Field: "position", Code: domain.CodeCategoryPos})
	}
	if len(issues) > 0 {
		return CategoryView{}, &domain.ValidationError{Issues: issues}
	}
	return s.Repo.UpdateCategory(ctx, gymID, id, name, in.Position)
}

func (s *Service) gymExists(ctx context.Context, gymID uuid.UUID) error {
	ok, err := s.Directory.GymExists(ctx, gymID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrGymNotFound
	}
	return nil
}

// AdminCategories lists a gym's categories with how many products each has (superadmin).
func (s *Service) AdminCategories(ctx context.Context, gymID uuid.UUID) ([]AdminCategoryView, error) {
	if err := s.gymExists(ctx, gymID); err != nil {
		return nil, err
	}
	return s.Repo.Categories(ctx, gymID)
}

// AdminCreateCategories adds many categories at once (superadmin), skipping the names the gym already has
// and the ones repeated in the list, ignoring case.
func (s *Service) AdminCreateCategories(ctx context.Context, gymID uuid.UUID, in BulkCategoriesInput) (BulkCategoriesResult, error) {
	if err := s.gymExists(ctx, gymID); err != nil {
		return BulkCategoriesResult{}, err
	}
	names, repeated, ok := domain.CategoryNames(in.Names)
	if !ok {
		return BulkCategoriesResult{}, invalid("names", domain.CodeCategoryNames)
	}
	created, skipped, err := s.Repo.CreateCategories(ctx, gymID, names)
	if err != nil {
		return BulkCategoriesResult{}, err
	}
	return BulkCategoriesResult{Created: created, Skipped: append(skipped, repeated...)}, nil
}

// AdminUpdateCategory renames or moves a category of a gym (superadmin).
func (s *Service) AdminUpdateCategory(ctx context.Context, gymID, id uuid.UUID, in CategoryChanges) (CategoryView, error) {
	if err := s.gymExists(ctx, gymID); err != nil {
		return CategoryView{}, err
	}
	return s.updateCategory(ctx, gymID, id, in)
}

// AdminDeleteCategory removes a category of a gym (superadmin).
func (s *Service) AdminDeleteCategory(ctx context.Context, gymID, id uuid.UUID) error {
	if err := s.gymExists(ctx, gymID); err != nil {
		return err
	}
	return s.Repo.DeleteCategory(ctx, gymID, id)
}
