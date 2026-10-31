package integration_test

import (
	"context"
	"database/sql"
	"io/fs"
	"net/url"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/db"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/testsupport"
)

// TestMigrationFromGymPricesToPlans runs the migrations in a throwaway schema up to the old shape (one price
// list per gym), inserts data, applies 00015 and checks that nothing is lost; then goes back down.
func TestMigrationFromGymPricesToPlans(t *testing.T) {
	ctx := context.Background()
	base, err := url.Parse(testsupport.DatabaseURL(t))
	require.NoError(t, err)
	schema := "mig_" + ids.New().String()[24:]
	admin, err := sql.Open("pgx", base.String())
	require.NoError(t, err)
	_, err = admin.ExecContext(ctx, `CREATE SCHEMA `+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
		_ = admin.Close()
	})

	query := base.Query()
	query.Set("search_path", schema)
	base.RawQuery = query.Encode()
	conn, err := sql.Open("pgx", base.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetMaxOpenConns(1) // one session: the search_path and the data stay together
	migrations, err := fs.Sub(db.Migrations, "migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectPostgres, conn, migrations)
	require.NoError(t, err)

	_, err = provider.UpTo(ctx, 14)
	require.NoError(t, err)
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := conn.ExecContext(ctx, sql, args...)
		require.NoError(t, err)
	}
	group, withPrices, withoutPrices := ids.New(), ids.New(), ids.New()
	branchA, branchB := ids.New(), ids.New()
	exec(`INSERT INTO groups (id, name) VALUES ($1, 'Grupo')`, group)
	exec(`INSERT INTO gyms (id, group_id, name, auth_tenant_id, auth_client_id, membership_prices) VALUES
		($1, $3, 'Con tarifas', gen_random_uuid(), gen_random_uuid(), '[{"months":3,"amount":"270.00"},{"months":1,"amount":"100.00"}]'),
		($2, $3, 'Sin tarifas', gen_random_uuid(), gen_random_uuid(), '[]')`, withPrices, withoutPrices, group)
	exec(`INSERT INTO branches (id, gym_id, name) VALUES ($1, $3, 'A'), ($2, $4, 'B')`, branchA, branchB, withPrices, withoutPrices)
	member, other := ids.New(), ids.New()
	exec(`INSERT INTO members (id, gym_id, home_branch_id, document_type, document_number, full_name, phone) VALUES
		($1, $3, $5, 'dni', '90000001', 'Ana Antigua', '999111222'), ($2, $4, $6, 'dni', '90000002', 'Beto Antiguo', '999111223')`,
		member, other, withPrices, withoutPrices, branchA, branchB)
	exec(`INSERT INTO memberships (id, gym_id, member_id, months, starts_on, ends_on, amount, access_scope) VALUES
		($1, $3, $4, 3, '2026-01-01', '2026-04-01', 270, 'branch'), ($2, $5, $6, 1, '2026-01-01', '2026-02-01', 90, 'gym')`,
		ids.New(), ids.New(), withPrices, member, withoutPrices, other)

	_, err = provider.UpTo(ctx, 15)
	require.NoError(t, err)

	// Every gym got its default plan; the old list became the prices of the plan.
	var plans int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM membership_plans`).Scan(&plans))
	assert.Equal(t, 2, plans)
	rows, err := conn.QueryContext(ctx, `SELECT months, amount::text FROM membership_plan_prices pr
		JOIN membership_plans p ON p.id = pr.plan_id WHERE p.gym_id = $1 ORDER BY months`, withPrices)
	require.NoError(t, err)
	var got [][2]any
	for rows.Next() {
		var months int
		var amount string
		require.NoError(t, rows.Scan(&months, &amount))
		got = append(got, [2]any{months, amount})
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	assert.Equal(t, [][2]any{{1, "100.00"}, {3, "270.00"}}, got)
	var priced int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM membership_plan_prices pr JOIN membership_plans p ON p.id = pr.plan_id WHERE p.gym_id = $1`, withoutPrices).Scan(&priced))
	assert.Equal(t, 0, priced, "a gym without prices gets a plan without prices")

	// Every period points to its gym's default plan and keeps its name; plans never cross gyms.
	var linked int
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT count(*) FROM memberships ms JOIN membership_plans p ON p.id = ms.plan_id AND p.gym_id = ms.gym_id
		WHERE p.name = 'Membresía' AND ms.plan_name = 'Membresía' AND p.is_active`).Scan(&linked))
	assert.Equal(t, 2, linked)
	var column bool
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'gyms' AND column_name = 'membership_prices')`, schema).Scan(&column))
	assert.False(t, column, "the old price list is gone")
	// The database refuses a period whose plan belongs to another gym.
	_, err = conn.ExecContext(ctx, `UPDATE memberships SET plan_id = (SELECT id FROM membership_plans WHERE gym_id = $1) WHERE gym_id = $2`, withPrices, withoutPrices)
	assert.Error(t, err)

	// Going back restores the list from the first plan.
	_, err = provider.Down(ctx)
	require.NoError(t, err)
	var restored string
	require.NoError(t, conn.QueryRowContext(ctx, `SELECT membership_prices::text FROM gyms WHERE id = $1`, withPrices).Scan(&restored))
	assert.JSONEq(t, `[{"months":1,"amount":"100.00"},{"months":3,"amount":"270.00"}]`, restored)
}
