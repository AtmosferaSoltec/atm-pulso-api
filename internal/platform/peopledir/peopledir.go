// Package peopledir reads members and staff users (tables owned by the members and accounts modules) with plain
// SELECTs, to show names next to ids and to check that a member belongs to the gym. It writes nothing; modules never
// import each other. Every query is limited to the gym it is given.
package peopledir

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound: the member is not in the gym.
var ErrNotFound = errors.New("person not found")

// Person is a member or a staff user as shown next to a record.
type Person struct {
	ID       uuid.UUID
	FullName string
	// DocumentNumber is the member's document (empty for staff).
	DocumentNumber string
}

// Reader reads people of a gym.
type Reader struct{ pool *pgxpool.Pool }

// New builds the reader.
func New(pool *pgxpool.Pool) *Reader { return &Reader{pool: pool} }

// Member returns one member of the gym, or ErrNotFound.
func (r *Reader) Member(ctx context.Context, gymID, memberID uuid.UUID) (Person, error) {
	var p Person
	err := r.pool.QueryRow(ctx, `SELECT id, full_name, document_number FROM members WHERE gym_id=$1 AND id=$2`, gymID, memberID).
		Scan(&p.ID, &p.FullName, &p.DocumentNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return Person{}, ErrNotFound
	}
	return p, err
}

// Members returns the listed members of the gym by id (the ones that do not exist are missing from the map).
func (r *Reader) Members(ctx context.Context, gymID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Person, error) {
	return r.many(ctx, `SELECT id, full_name, document_number FROM members WHERE gym_id=$1 AND id = ANY($2)`, gymID, ids)
}

// Users returns the listed staff users of the gym by id.
func (r *Reader) Users(ctx context.Context, gymID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Person, error) {
	return r.many(ctx, `SELECT id, full_name, '' FROM users WHERE gym_id=$1 AND id = ANY($2)`, gymID, ids)
}

func (r *Reader) many(ctx context.Context, query string, gymID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Person, error) {
	out := make(map[uuid.UUID]Person, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, query, gymID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.ID, &p.FullName, &p.DocumentNumber); err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}
