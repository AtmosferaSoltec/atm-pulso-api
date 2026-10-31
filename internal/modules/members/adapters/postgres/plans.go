package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

// DefaultPlanName is the plan a gym gets when it sells before configuring any.
const DefaultPlanName = "Membresía"

const planColumns = `id, name, description, is_active, position`

func readPlans(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, gymID uuid.UUID, onlyID *uuid.UUID, activeOnly bool) ([]app.Plan, error) {
	rows, err := q.Query(ctx, `SELECT `+planColumns+` FROM membership_plans
		WHERE gym_id = $1 AND ($2::uuid IS NULL OR id = $2) AND (NOT $3 OR is_active)
		ORDER BY position, created_at, id`, gymID, onlyID, activeOnly)
	if err != nil {
		return nil, err
	}
	plans := []app.Plan{}
	index := map[uuid.UUID]int{}
	for rows.Next() {
		plan := app.Plan{Prices: []app.PlanPrice{}}
		if err := rows.Scan(&plan.ID, &plan.Name, &plan.Description, &plan.IsActive, &plan.Position); err != nil {
			rows.Close()
			return nil, err
		}
		index[plan.ID] = len(plans)
		plans = append(plans, plan)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return plans, nil
	}
	ids := make([]uuid.UUID, 0, len(plans))
	for _, plan := range plans {
		ids = append(ids, plan.ID)
	}
	prices, err := q.Query(ctx, `SELECT plan_id, months, amount::text FROM membership_plan_prices
		WHERE gym_id = $1 AND plan_id = ANY($2) ORDER BY months`, gymID, ids)
	if err != nil {
		return nil, err
	}
	defer prices.Close()
	for prices.Next() {
		var planID uuid.UUID
		var price app.PlanPrice
		if err := prices.Scan(&planID, &price.Months, &price.Amount); err != nil {
			return nil, err
		}
		plans[index[planID]].Prices = append(plans[index[planID]].Prices, price)
	}
	return plans, prices.Err()
}

// Plans lists the gym's plans in the order the gym wants them offered.
func (r *Repository) Plans(ctx context.Context, gymID uuid.UUID, activeOnly bool) ([]app.Plan, error) {
	return readPlans(ctx, r.pool, gymID, nil, activeOnly)
}

func writePrices(ctx context.Context, t pgx.Tx, gymID, planID uuid.UUID, prices []app.PlanPrice) error {
	if _, err := t.Exec(ctx, `DELETE FROM membership_plan_prices WHERE plan_id = $1 AND gym_id = $2`, planID, gymID); err != nil {
		return err
	}
	for _, price := range prices {
		if _, err := t.Exec(ctx, `INSERT INTO membership_plan_prices (plan_id, gym_id, months, amount) VALUES ($1, $2, $3, $4::numeric)`,
			planID, gymID, price.Months, price.Amount); err != nil {
			return err
		}
	}
	return nil
}

func translatePlanWrite(err error) error {
	switch constraintOf(err) {
	case "membership_plans_name_key":
		return domain.ErrPlanNameTaken
	case "membership_plans_pkey", "membership_plans_id_gym_key":
		return domain.ErrConflict
	}
	return err
}

// CreatePlan stores a plan with its prices. A negative position puts it last.
func (r *Repository) CreatePlan(ctx context.Context, gymID uuid.UUID, plan app.Plan) (app.Plan, error) {
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		if _, err := t.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, gymID.String()); err != nil {
			return err
		}
		if _, err := t.Exec(ctx, `INSERT INTO membership_plans (id, gym_id, name, description, is_active, position)
			VALUES ($1, $2, $3, $4, $5, CASE WHEN $6 < 0 THEN COALESCE((SELECT max(position) + 1 FROM membership_plans WHERE gym_id = $2), 0) ELSE $6 END)`,
			plan.ID, gymID, plan.Name, plan.Description, plan.IsActive, plan.Position); err != nil {
			return err
		}
		return writePrices(ctx, t, gymID, plan.ID, plan.Prices)
	})
	if err != nil {
		return app.Plan{}, translatePlanWrite(err)
	}
	return r.onePlan(ctx, gymID, plan.ID)
}

func (r *Repository) onePlan(ctx context.Context, gymID, planID uuid.UUID) (app.Plan, error) {
	plans, err := readPlans(ctx, r.pool, gymID, &planID, false)
	if err != nil {
		return app.Plan{}, err
	}
	if len(plans) == 0 {
		return app.Plan{}, domain.ErrPlanNotFound
	}
	return plans[0], nil
}

// UpdatePlan applies the fields sent; prices replace the whole list.
func (r *Repository) UpdatePlan(ctx context.Context, gymID, planID uuid.UUID, c app.PlanChanges) (app.Plan, error) {
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		tag, err := t.Exec(ctx, `UPDATE membership_plans SET
			name = CASE WHEN $3 THEN $4 ELSE name END,
			description = CASE WHEN $5 THEN $6 ELSE description END,
			is_active = CASE WHEN $7 THEN $8 ELSE is_active END,
			position = CASE WHEN $9 THEN $10 ELSE position END,
			updated_at = now()
			WHERE id = $1 AND gym_id = $2`, planID, gymID,
			c.Name.Set, c.Name.Value, c.Description.Set, c.Description.Value, c.IsActive.Set, c.IsActive.Value, c.Position.Set, c.Position.Value)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrPlanNotFound
		}
		if c.Prices.Set && c.Prices.Value != nil {
			return writePrices(ctx, t, gymID, planID, *c.Prices.Value)
		}
		return nil
	})
	if err != nil {
		return app.Plan{}, translatePlanWrite(err)
	}
	return r.onePlan(ctx, gymID, planID)
}

// resolvePlan picks the plan of a sale inside its transaction. With a requested plan it must be an
// active plan of the gym. Without one, the gym's single active plan is used; a gym that has never
// configured any gets the default plan "Membresía" created on the spot; otherwise the sale must say which.
func resolvePlan(ctx context.Context, t pgx.Tx, gymID uuid.UUID, requested *uuid.UUID) (uuid.UUID, string, error) {
	invalid := func(code string) error {
		return &domain.ValidationError{Issues: []domain.Issue{{Field: "plan_id", Code: code}}}
	}
	if requested != nil {
		var name string
		err := t.QueryRow(ctx, `SELECT name FROM membership_plans WHERE id = $1 AND gym_id = $2 AND is_active FOR SHARE`, *requested, gymID).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, "", invalid(domain.CodePlanInvalid)
		}
		return *requested, name, err
	}
	// Serialise the lazy creation of the default plan between concurrent first sales of a gym.
	if _, err := t.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, gymID.String()); err != nil {
		return uuid.Nil, "", err
	}
	rows, err := t.Query(ctx, `SELECT id, name, is_active FROM membership_plans WHERE gym_id = $1`, gymID)
	if err != nil {
		return uuid.Nil, "", err
	}
	var activeID uuid.UUID
	var activeName string
	active, total := 0, 0
	for rows.Next() {
		var id uuid.UUID
		var name string
		var isActive bool
		if err := rows.Scan(&id, &name, &isActive); err != nil {
			rows.Close()
			return uuid.Nil, "", err
		}
		total++
		if isActive {
			active, activeID, activeName = active+1, id, name
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return uuid.Nil, "", err
	}
	switch {
	case active == 1:
		return activeID, activeName, nil
	case total == 0:
		id := ids.New()
		_, err := t.Exec(ctx, `INSERT INTO membership_plans (id, gym_id, name, position) VALUES ($1, $2, $3, 0)`, id, gymID, DefaultPlanName)
		return id, DefaultPlanName, err
	default:
		return uuid.Nil, "", invalid(domain.CodePlanRequired)
	}
}
