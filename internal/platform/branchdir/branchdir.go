// Package branchdir reads the branches and gyms of an gym (tables owned by the
// accounts module) with plain SELECTs, for the modules that sell or check access in a
// branch. It writes nothing; modules never import each other.
package branchdir

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound: the branch is not in the gym.
var ErrNotFound = errors.New("branch not found")

// DefaultDayPassPrice applies when a gym has not set its own.
const DefaultDayPassPrice = "15.00"

// Info is a branch with its gym.
type Info struct {
	ID            uuid.UUID
	GymID         uuid.UUID
	Name          string
	GymName       string
	BranchSharing bool
	// DayPassPrice is the gym's effective price ("15.00" when it has none).
	DayPassPrice string
}

// Reader reads branches of an gym.
type Reader struct{ pool *pgxpool.Pool }

// New builds the reader.
func New(pool *pgxpool.Pool) *Reader { return &Reader{pool: pool} }

const selectBranch = `SELECT b.id, b.gym_id, b.name, COALESCE(g.display_name, g.name), g.branch_sharing,
		COALESCE(g.day_pass_price, $2::numeric)::text
	FROM branches b JOIN gyms g ON g.id = b.gym_id
	WHERE b.gym_id = $1`

func scan(row pgx.Row) (Info, error) {
	var b Info
	err := row.Scan(&b.ID, &b.GymID, &b.Name, &b.GymName, &b.BranchSharing, &b.DayPassPrice)
	return b, err
}

// Branch returns one branch of the gym, or ErrNotFound.
func (r *Reader) Branch(ctx context.Context, gymID, branchID uuid.UUID) (Info, error) {
	b, err := scan(r.pool.QueryRow(ctx, selectBranch+` AND b.id = $3`, gymID, DefaultDayPassPrice, branchID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Info{}, ErrNotFound
	}
	return b, err
}

// Branches returns every branch of the gym.
func (r *Reader) Branches(ctx context.Context, gymID uuid.UUID) ([]Info, error) {
	rows, err := r.pool.Query(ctx, selectBranch+` ORDER BY g.name, b.name, b.id`, gymID, DefaultDayPassPrice)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Info
	for rows.Next() {
		b, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
