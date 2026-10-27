// Package postgres reads payments, members, memberships and access entries (owned by
// other modules) with SELECTs only; the reports module has no tables.
package postgres

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/reports/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/receipts"
)

// Repository answers the report queries.
type Repository struct{ pool *pgxpool.Pool }

// NewRepository builds the repository.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const paymentFrom = `FROM payments p
	JOIN receipts r ON r.id = p.receipt_id AND r.organization_id = p.organization_id
	JOIN branches b ON b.id = p.branch_id AND b.organization_id = p.organization_id
	LEFT JOIN members m ON m.id = p.member_id AND m.organization_id = p.organization_id
	LEFT JOIN memberships ms ON ms.id = p.membership_id AND ms.organization_id = p.organization_id
	LEFT JOIN day_passes d ON d.id = p.day_pass_id AND d.organization_id = p.organization_id
	WHERE p.organization_id = $1 AND p.branch_id = ANY($2) AND p.paid_on BETWEEN $3 AND $4
	AND ($5::text = '' OR p.method = $5) AND ($6::text = '' OR p.kind = $6)
	AND ($7::text = '' OR m.full_name ILIKE '%' || $7 || '%' OR m.document_number LIKE '%' || $7 || '%' OR d.customer_name ILIKE '%' || $7 || '%')`

func args(org uuid.UUID, f app.PaymentFilter) []any {
	query := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(f.Query))
	return []any{org, f.BranchIDs, f.From, f.To, f.Method, f.Kind, query}
}

func planLabel(kind string, months int) string {
	switch {
	case kind == "day_pass":
		return "Pase del día"
	case months == 1:
		return "1 mes"
	default:
		return strconv.Itoa(months) + " meses"
	}
}

// Payments returns the rows of a filter, newest first, and the total number of matches.
func (r *Repository) Payments(ctx context.Context, org uuid.UUID, f app.PaymentFilter) ([]app.PaymentRow, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) `+paymentFrom, args(org, f)...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT p.id, p.paid_on, r.id, r.kind, r.number, COALESCE(ms.months, 0), p.amount::text, p.method,
			m.id, m.full_name, m.document_type, m.document_number, d.customer_name, b.id, b.name `+paymentFrom+
		` ORDER BY p.paid_on DESC, p.created_at DESC, p.id DESC LIMIT $8 OFFSET $9`, append(args(org, f), f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []app.PaymentRow{}
	for rows.Next() {
		var row app.PaymentRow
		var paid time.Time
		var number, months int
		var kind string
		var memberID *uuid.UUID
		var memberName *string
		if err := rows.Scan(&row.ID, &paid, &row.Receipt.ID, &kind, &number, &months, &row.Amount, &row.Method,
			&memberID, &memberName, &row.DocumentType, &row.Document, &row.VisitorName, &row.Branch.ID, &row.Branch.Name); err != nil {
			return nil, 0, err
		}
		row.PaidOn, row.Kind = paid.Format("2006-01-02"), kind
		row.Receipt.Number = receipts.Label(receipts.Kind(kind), number)
		row.PlanLabel = planLabel(kind, months)
		if memberID != nil && memberName != nil {
			row.Member = &app.MemberRef{ID: *memberID, FullName: *memberName}
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

var methodOrder = []string{"yape_plin", "cash", "card", "transfer"}

// Totals are the KPIs of the whole filter, not just one page.
func (r *Repository) Totals(ctx context.Context, org uuid.UUID, f app.PaymentFilter) (app.Totals, error) {
	t := app.Totals{ByMethod: map[string]string{}}
	var sums [4]string
	var counts [4]int
	var average *string
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(sum(p.amount), 0)::numeric(14,2)::text, count(*), count(*) FILTER (WHERE p.kind = 'day_pass'),
			CASE WHEN count(*) = 0 THEN NULL ELSE round(sum(p.amount) / count(*), 2)::text END,
			COALESCE(sum(p.amount) FILTER (WHERE p.method = 'yape_plin'), 0)::numeric(14,2)::text, COALESCE(sum(p.amount) FILTER (WHERE p.method = 'cash'), 0)::numeric(14,2)::text,
			COALESCE(sum(p.amount) FILTER (WHERE p.method = 'card'), 0)::numeric(14,2)::text, COALESCE(sum(p.amount) FILTER (WHERE p.method = 'transfer'), 0)::numeric(14,2)::text,
			count(*) FILTER (WHERE p.method = 'yape_plin'), count(*) FILTER (WHERE p.method = 'cash'),
			count(*) FILTER (WHERE p.method = 'card'), count(*) FILTER (WHERE p.method = 'transfer') `+paymentFrom, args(org, f)...).
		Scan(&t.Amount, &t.Count, &t.DayPasses, &average, &sums[0], &sums[1], &sums[2], &sums[3], &counts[0], &counts[1], &counts[2], &counts[3])
	if err != nil {
		return t, err
	}
	t.Average = average
	best := 0
	for i, method := range methodOrder {
		t.ByMethod[method] = sums[i]
		if counts[i] > best {
			best = counts[i]
			name := method
			t.TopMethod = &name
		}
	}
	return t, nil
}

// Dashboard computes the figures of a set of branches for a Lima day and month.
func (r *Repository) Dashboard(ctx context.Context, org uuid.UUID, branchIDs []uuid.UUID, today, monthStart time.Time) (app.DashboardView, error) {
	v := app.DashboardView{Date: today.Format("2006-01-02"), ExpiringSoon: []app.ExpiringItem{}}
	const base = `WITH base AS (
		SELECT m.id, m.full_name, m.phone, s.latest_end,
		       CASE WHEN COALESCE(s.active_now, false) THEN CASE WHEN s.latest_end - $3::date <= 7 THEN 'expiring' ELSE 'active' END
		            WHEN COALESCE(s.scheduled, false) THEN 'scheduled' ELSE 'expired' END AS status
		FROM members m
		LEFT JOIN LATERAL (SELECT max(ends_on) AS latest_end,
				bool_or(starts_on <= $3::date AND ends_on >= $3::date) AS active_now, bool_or(starts_on > $3::date) AS scheduled
			FROM memberships WHERE member_id = m.id) s ON true
		WHERE m.organization_id = $1 AND m.home_branch_id = ANY($2))`
	err := r.pool.QueryRow(ctx, base+` SELECT count(*) FILTER (WHERE status = 'active'), count(*) FILTER (WHERE status = 'expiring'),
			count(*) FILTER (WHERE status = 'expired'), count(*) FILTER (WHERE status = 'scheduled') FROM base`, org, branchIDs, today).
		Scan(&v.Members.Active, &v.Members.Expiring, &v.Members.Expired, &v.Members.Scheduled)
	if err != nil {
		return v, err
	}
	err = r.pool.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER (WHERE paid_on = $3), 0)::numeric(14,2)::text, COALESCE(sum(amount), 0)::numeric(14,2)::text
		FROM payments WHERE organization_id = $1 AND branch_id = ANY($2) AND paid_on BETWEEN $4 AND $3`, org, branchIDs, today, monthStart).
		Scan(&v.Income.Today, &v.Income.Month)
	if err != nil {
		return v, err
	}
	start, end := today.Add(5*time.Hour), today.AddDate(0, 0, 1).Add(5*time.Hour)
	err = r.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE allowed), count(*) FILTER (WHERE NOT allowed) FROM access_entries
		WHERE organization_id = $1 AND branch_id = ANY($2) AND kind = 'check' AND checked_at >= $3 AND checked_at < $4`, org, branchIDs, start, end).
		Scan(&v.Access.AllowedToday, &v.Access.DeniedToday)
	if err != nil {
		return v, err
	}
	rows, err := r.pool.Query(ctx, base+` SELECT id, full_name, latest_end, latest_end - $3::date, phone FROM base WHERE status = 'expiring'
		ORDER BY latest_end, full_name LIMIT 5`, org, branchIDs, today)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var item app.ExpiringItem
		var ends time.Time
		if err := rows.Scan(&item.MemberID, &item.FullName, &ends, &item.DaysRemaining, &item.Phone); err != nil {
			return v, err
		}
		item.EndsOn = ends.Format("2006-01-02")
		v.ExpiringSoon = append(v.ExpiringSoon, item)
	}
	return v, rows.Err()
}
