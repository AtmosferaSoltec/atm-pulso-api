package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Fixed ids so the demo data can be loaded again without duplicating anything.
var (
	orgAtlas   = uuid.MustParse("019a0000-0000-7000-8000-000000000001")
	orgCentral = uuid.MustParse("019a0000-0000-7000-8000-000000000002")

	gymAtlasFitness = uuid.MustParse("019a0000-0000-7000-8000-000000000011")
	gymNorte        = uuid.MustParse("019a0000-0000-7000-8000-000000000012")
	gymDistrito     = uuid.MustParse("019a0000-0000-7000-8000-000000000013")
	gymCentral      = uuid.MustParse("019a0000-0000-7000-8000-000000000014")

	branchMiraflores = uuid.MustParse("019a0000-0000-7000-8000-000000000021")
	branchBarranco   = uuid.MustParse("019a0000-0000-7000-8000-000000000022")
	branchOlivos     = uuid.MustParse("019a0000-0000-7000-8000-000000000023")
	branchSanMiguel  = uuid.MustParse("019a0000-0000-7000-8000-000000000024")
	branchCentral    = uuid.MustParse("019a0000-0000-7000-8000-000000000025")
)

// SeedDemo loads the same fictional accounts the web prototype shows, so the web and the
// API can be tried together. Development only: every user gets the same password.
func SeedDemo(ctx context.Context, pool *pgxpool.Pool, passwordHash string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organizations (id, name) VALUES ($1, 'Grupo Atlas'), ($2, 'Gimnasio Central') ON CONFLICT (id) DO NOTHING`, []any{orgAtlas, orgCentral}},
		{`INSERT INTO gyms (id, organization_id, name, tax_id, brand_color, day_pass_price) VALUES
			($1, $5, 'Atlas Fitness', 'DEMO-001', '#17677b', 20),
			($2, $5, 'Norte Gym', 'DEMO-002', '#356443', 15),
			($3, $5, 'Distrito Fuerza', 'DEMO-003', '#7751a3', 15),
			($4, $6, 'Gimnasio Central', 'DEMO-004', '#ad522a', 12)
			ON CONFLICT (id) DO NOTHING`, []any{gymAtlasFitness, gymNorte, gymDistrito, gymCentral, orgAtlas, orgCentral}},
		{`INSERT INTO branches (id, gym_id, name, address) VALUES
			($1, $6, 'Miraflores', 'Av. de Ejemplo 101'),
			($2, $6, 'Barranco', 'Calle de Ejemplo 202'),
			($3, $7, 'Los Olivos', 'Av. de Ejemplo 303'),
			($4, $8, 'San Miguel', 'Calle de Ejemplo 404'),
			($5, $9, 'Sede principal', 'Av. de Ejemplo 505')
			ON CONFLICT (id) DO NOTHING`, []any{branchMiraflores, branchBarranco, branchOlivos, branchSanMiguel, branchCentral, gymAtlasFitness, gymNorte, gymDistrito, gymCentral}},
		{`INSERT INTO users (id, organization_id, gym_id, document_number, full_name, role, password_hash) VALUES
			('019a0000-0000-7000-8000-000000000101', NULL, NULL, '10000001', 'Equipo Atmósfera', 'superadmin', $1),
			('019a0000-0000-7000-8000-000000000102', $2, NULL, '10000002', 'Andrea Salas', 'owner', $1),
			('019a0000-0000-7000-8000-000000000103', $2, NULL, '10000003', 'Luis Rojas', 'receptionist', $1),
			('019a0000-0000-7000-8000-000000000104', $3, NULL, '10000004', 'Paola Vega', 'owner', $1),
			('019a0000-0000-7000-8000-000000000105', $2, $4, '10000005', 'Carlos Medina', 'admin', $1)
			ON CONFLICT (id) DO NOTHING`, []any{passwordHash, orgAtlas, orgCentral, gymAtlasFitness}},
		{`INSERT INTO user_branches (user_id, branch_id) VALUES
			('019a0000-0000-7000-8000-000000000103', $1),
			('019a0000-0000-7000-8000-000000000103', $2)
			ON CONFLICT DO NOTHING`, []any{branchMiraflores, branchBarranco}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			return fmt.Errorf("seed: %w", err)
		}
	}
	return tx.Commit(ctx)
}
