// Package postgres is the persistence adapter of access. It reads members, memberships,
// branches and gyms (owned by other modules) with SELECTs only.
package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/receipts"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

// Repository stores access entries and day passes.
type Repository struct{ pool *pgxpool.Pool }

// NewRepository builds the repository.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// limaOffset converts a Lima date (midnight UTC) to the instant that day starts in Lima.
const limaOffset = 5 * time.Hour

func dayStart(d time.Time) time.Time { return d.Add(limaOffset) }
func dayEnd(d time.Time) time.Time   { return d.AddDate(0, 0, 1).Add(limaOffset) }

// Member finds a member by id or document with every period and the branches it opens.
func (r *Repository) Member(ctx context.Context, gymID uuid.UUID, id *uuid.UUID, document string) (app.MemberAccess, error) {
	var m app.MemberAccess
	err := r.pool.QueryRow(ctx, `SELECT m.id, m.full_name, m.document_number, m.home_branch_id, g.branch_sharing
		FROM members m
		JOIN branches b ON b.id = m.home_branch_id AND b.gym_id = m.gym_id
		JOIN gyms g ON g.id = b.gym_id
		WHERE m.gym_id = $1 AND (($2::uuid IS NOT NULL AND m.id = $2) OR ($2::uuid IS NULL AND m.document_number = $3))`,
		gymID, id, document).Scan(&m.ID, &m.FullName, &m.DocumentNumber, &m.HomeBranchID, &m.HomeSharing)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, domain.ErrMemberNotFound
	}
	if err != nil {
		return m, err
	}
	rows, err := r.pool.Query(ctx, `SELECT ms.starts_on, ms.ends_on,
			COALESCE(array_agg(mb.branch_id) FILTER (WHERE mb.branch_id IS NOT NULL), '{}')
		FROM memberships ms LEFT JOIN membership_branches mb ON mb.membership_id = ms.id
		WHERE ms.member_id = $1 AND ms.gym_id = $2 GROUP BY ms.id`, m.ID, gymID)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var p domain.Period
		if err := rows.Scan(&p.StartsOn, &p.EndsOn, &p.BranchIDs); err != nil {
			return m, err
		}
		m.Periods = append(m.Periods, p)
	}
	return m, rows.Err()
}

const entryColumns = `id, gym_id, branch_id, member_id, display_name, document_number, allowed, reason_code, kind, ends_on, expiring_in_days, checked_by, checked_at`

// Entry returns one access entry.
func (r *Repository) Entry(ctx context.Context, gymID, id uuid.UUID) (app.Entry, error) {
	var e app.Entry
	err := r.pool.QueryRow(ctx, `SELECT `+entryColumns+` FROM access_entries WHERE id=$1 AND gym_id=$2`, id, gymID).
		Scan(&e.ID, &e.GymID, &e.BranchID, &e.MemberID, &e.DisplayName, &e.DocumentNumber, &e.Allowed, &e.Reason, &e.Kind, &e.EndsOn, &e.ExpiringInDays, &e.CheckedBy, &e.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, domain.ErrNotFound
	}
	return e, err
}

func insertEntry(ctx context.Context, q interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, e app.Entry) error {
	_, err := q.Exec(ctx, `INSERT INTO access_entries (id, gym_id, branch_id, member_id, display_name, document_number, allowed, reason_code, kind, ends_on, expiring_in_days, checked_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		e.ID, e.GymID, e.BranchID, e.MemberID, e.DisplayName, e.DocumentNumber, e.Allowed, e.Reason, e.Kind, e.EndsOn, e.ExpiringInDays, e.CheckedBy)
	if isUnique(err) {
		return domain.ErrConflict
	}
	return err
}

// InsertEntry logs an access attempt.
func (r *Repository) InsertEntry(ctx context.Context, e app.Entry) error {
	return insertEntry(ctx, r.pool, e)
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func like(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(q))
}

func bounds(from, to *time.Time) (*time.Time, *time.Time) {
	var start, end *time.Time
	if from != nil {
		s := dayStart(*from)
		start = &s
	}
	if to != nil {
		e := dayEnd(*to)
		end = &e
	}
	return start, end
}

// Events lists the history of the given branches.
func (r *Repository) Events(ctx context.Context, gymID uuid.UUID, f app.EventFilter) ([]app.EventView, int, error) {
	start, end := bounds(f.From, f.To)
	const where = `FROM access_entries e JOIN branches b ON b.id = e.branch_id AND b.gym_id = e.gym_id
		WHERE e.gym_id = $1 AND e.branch_id = ANY($2)
		AND ($3::text = '' OR e.allowed = ($3 = 'allowed'))
		AND ($4::timestamptz IS NULL OR e.checked_at >= $4) AND ($5::timestamptz IS NULL OR e.checked_at < $5)
		AND ($6::text = '' OR e.display_name ILIKE '%' || $6 || '%' OR e.document_number LIKE '%' || $6 || '%')`
	args := []any{gymID, f.BranchIDs, f.Result, start, end, like(f.Query)}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT e.id, e.checked_at, b.id, b.name, e.kind, e.allowed, e.reason_code, e.display_name, e.document_number, e.member_id `+
		where+` ORDER BY e.checked_at DESC, e.id DESC LIMIT $7 OFFSET $8`, append(args, f.PageSize, (f.Page-1)*f.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []app.EventView{}
	for rows.Next() {
		var v app.EventView
		if err := rows.Scan(&v.ID, &v.CheckedAt, &v.Branch.ID, &v.Branch.Name, &v.Kind, &v.Allowed, &v.Reason, &v.DisplayName, &v.DocumentNumber, &v.MemberID); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// Summary counts checks and day passes between two Lima dates (inclusive).
func (r *Repository) Summary(ctx context.Context, gymID uuid.UUID, branchIDs []uuid.UUID, from, to time.Time) (app.Summary, error) {
	var s app.Summary
	start, end := dayStart(from), dayEnd(to)
	err := r.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE kind = 'check' AND allowed), count(*) FILTER (WHERE kind = 'check' AND NOT allowed)
		FROM access_entries WHERE gym_id = $1 AND branch_id = ANY($2) AND checked_at >= $3 AND checked_at < $4`,
		gymID, branchIDs, start, end).Scan(&s.Allowed, &s.Denied)
	if err != nil {
		return s, err
	}
	err = r.pool.QueryRow(ctx, `SELECT count(*) FROM day_passes WHERE gym_id = $1 AND branch_id = ANY($2) AND created_at >= $3 AND created_at < $4`,
		gymID, branchIDs, start, end).Scan(&s.DayPasses)
	return s, err
}

func nullable(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// SellDayPass stores the day pass, its receipt, payment and the allowed entry in one transaction.
func (r *Repository) SellDayPass(ctx context.Context, sale app.DayPassSale) (bool, error) {
	existed := false
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		var one int
		err := t.QueryRow(ctx, `SELECT 1 FROM day_passes WHERE id=$1 AND gym_id=$2`, sale.ID, sale.GymID).Scan(&one)
		if err == nil {
			existed = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		receiptID, _, err := receipts.Issue(ctx, t, sale.GymID, receipts.DayPass, sale.BranchID, sale.Today)
		if err != nil {
			return err
		}
		v := sale.Visitor
		if _, err := t.Exec(ctx, `INSERT INTO day_passes (id, gym_id, branch_id, receipt_id, amount, method, customer_document_type, customer_document_number, customer_name, customer_phone, created_by)
			VALUES ($1,$2,$3,$4,$5::numeric,$6,$7,$8,$9,$10,$11)`, sale.ID, sale.GymID, sale.BranchID, receiptID, sale.Amount, sale.Method,
			nullable(v.DocumentType), nullable(v.DocumentNumber), nullable(strings.Join(strings.Fields(v.FullName), " ")), nullable(v.Phone), sale.ActorID); err != nil {
			return err
		}
		if err := receipts.Record(ctx, t, receipts.Payment{GymID: sale.GymID, ReceiptID: receiptID, BranchID: sale.BranchID, Kind: receipts.DayPass,
			Method: sale.Method, Amount: sale.Amount, PaidOn: sale.Today, DayPassID: &sale.ID, CreatedBy: sale.ActorID}); err != nil {
			return err
		}
		name := "Pase del día"
		if n := nullable(strings.Join(strings.Fields(v.FullName), " ")); n != nil {
			name = *n
		}
		return insertEntry(ctx, t, app.Entry{ID: uuid.New(), GymID: sale.GymID, BranchID: sale.BranchID, DisplayName: name,
			DocumentNumber: nullable(v.DocumentNumber), Allowed: true, Reason: domain.ReasonDayPass, Kind: "day_pass", CheckedBy: sale.ActorID})
	})
	if isUnique(err) {
		return false, domain.ErrConflict
	}
	return existed, err
}

const dayPassSelect = `SELECT d.id, r.id, r.number, b.id, b.name, d.amount::text, d.method, d.customer_name, d.created_at
	FROM day_passes d JOIN receipts r ON r.id = d.receipt_id AND r.gym_id = d.gym_id
	JOIN branches b ON b.id = d.branch_id AND b.gym_id = d.gym_id`

func scanDayPass(row pgx.Row) (app.DayPassView, error) {
	var v app.DayPassView
	var number int
	err := row.Scan(&v.ID, &v.Receipt.ID, &number, &v.Branch.ID, &v.Branch.Name, &v.Amount, &v.Method, &v.VisitorName, &v.CreatedAt)
	v.Receipt.Number = receipts.Label(receipts.DayPass, number)
	return v, err
}

// DayPass returns one day pass.
func (r *Repository) DayPass(ctx context.Context, gymID, id uuid.UUID) (app.DayPassView, error) {
	v, err := scanDayPass(r.pool.QueryRow(ctx, dayPassSelect+` WHERE d.id = $1 AND d.gym_id = $2`, id, gymID))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, domain.ErrNotFound
	}
	return v, err
}

// DayPasses lists day passes of the given branches, newest first.
func (r *Repository) DayPasses(ctx context.Context, gymID uuid.UUID, f app.DayPassFilter) ([]app.DayPassView, int, error) {
	start, end := bounds(f.From, f.To)
	const where = ` WHERE d.gym_id = $1 AND d.branch_id = ANY($2)
		AND ($3::timestamptz IS NULL OR d.created_at >= $3) AND ($4::timestamptz IS NULL OR d.created_at < $4)`
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM day_passes d`+where, gymID, f.BranchIDs, start, end).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, dayPassSelect+where+` ORDER BY d.created_at DESC, d.id DESC LIMIT $5 OFFSET $6`,
		gymID, f.BranchIDs, start, end, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []app.DayPassView{}
	for rows.Next() {
		v, err := scanDayPass(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
