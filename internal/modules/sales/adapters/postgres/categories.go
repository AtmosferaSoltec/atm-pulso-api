package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

// Categories lists the gym's categories with their product count.
func (r *Repository) Categories(ctx context.Context, gymID uuid.UUID) ([]app.AdminCategoryView, error) {
	rows, err := r.pool.Query(ctx, `SELECT c.id, c.name, c.position,
		(SELECT count(*) FROM products p WHERE p.gym_id = c.gym_id AND p.category_id = c.id)
		FROM product_categories c WHERE c.gym_id = $1 ORDER BY c.position, lower(c.name), c.id`, gymID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.AdminCategoryView{}
	for rows.Next() {
		var v app.AdminCategoryView
		if err := rows.Scan(&v.ID, &v.Name, &v.Position, &v.ProductCount); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CreateCategories appends the names after the last position under a per-gym lock; a name the gym already
// has (ignoring case) is skipped by the unique index.
func (r *Repository) CreateCategories(ctx context.Context, gymID uuid.UUID, names []string) ([]app.CategoryView, []string, error) {
	created := []app.CategoryView{}
	skipped := []string{}
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		created, skipped = created[:0], skipped[:0]
		if _, err := t.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 2))`, gymID.String()); err != nil {
			return err
		}
		var next int
		if err := t.QueryRow(ctx, `SELECT COALESCE(max(position) + 1, 0) FROM product_categories WHERE gym_id = $1`, gymID).Scan(&next); err != nil {
			return err
		}
		for _, name := range names {
			var v app.CategoryView
			err := t.QueryRow(ctx, `INSERT INTO product_categories (id, gym_id, name, position) VALUES ($1, $2, $3, $4)
				ON CONFLICT (gym_id, lower(name)) DO NOTHING RETURNING id, name, position`, ids.New(), gymID, name, next).
				Scan(&v.ID, &v.Name, &v.Position)
			if errors.Is(err, pgx.ErrNoRows) {
				skipped = append(skipped, name)
				continue
			}
			if err != nil {
				return err
			}
			created = append(created, v)
			next++
		}
		return nil
	})
	return created, skipped, err
}

// UpdateCategory applies the fields sent.
func (r *Repository) UpdateCategory(ctx context.Context, gymID, id uuid.UUID, name *string, position *int) (app.CategoryView, error) {
	var v app.CategoryView
	err := r.pool.QueryRow(ctx, `UPDATE product_categories SET name = COALESCE($3, name), position = COALESCE($4, position),
		updated_at = now() WHERE id = $1 AND gym_id = $2 RETURNING id, name, position`, id, gymID, name, position).
		Scan(&v.ID, &v.Name, &v.Position)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return v, domain.ErrCategoryNotFound
	case constraintOf(err) == "product_categories_name_key":
		return v, domain.ErrCategoryNameTaken
	}
	return v, err
}

// DeleteCategory removes the category; the foreign key leaves its products without one (ON DELETE SET NULL).
func (r *Repository) DeleteCategory(ctx context.Context, gymID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM product_categories WHERE id = $1 AND gym_id = $2`, id, gymID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCategoryNotFound
	}
	return nil
}
