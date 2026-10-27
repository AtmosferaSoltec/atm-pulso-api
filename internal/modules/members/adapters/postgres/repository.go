// Package postgres is the persistence adapter of members.
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

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/receipts"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

// Repository stores members, memberships and their receipts.
type Repository struct{ pool *pgxpool.Pool }

// NewRepository builds the repository.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func formatDate(t time.Time) string { return t.Format("2006-01-02") }

// statusBase computes only the status in SQL (a cheap aggregate per member), so filtering, counting
// and paging happen before the heavier per-row details are looked up.
// $1 organization, $2 today (Lima), $3 search text, $4 home branch (or NULL), $5 member id (or NULL).
const statusBase = `
WITH base AS (
  SELECT m.id, m.document_type, m.document_number, m.full_name, m.phone, m.created_at, m.home_branch_id,
         CASE WHEN COALESCE(s.active_now, false) THEN
                CASE WHEN s.latest_end - $2::date <= 7 THEN 'expiring' ELSE 'active' END
              WHEN COALESCE(s.scheduled, false) THEN 'scheduled'
              ELSE 'expired' END AS status,
         COALESCE(s.latest_end - $2::date, 0) AS days_remaining
  FROM members m
  LEFT JOIN LATERAL (
    SELECT max(ends_on) AS latest_end,
           bool_or(starts_on <= $2::date AND ends_on >= $2::date) AS active_now,
           bool_or(starts_on > $2::date) AS scheduled
    FROM memberships WHERE member_id = m.id) s ON true
  WHERE m.organization_id = $1
    AND ($3::text = '' OR m.full_name ILIKE '%' || $3 || '%' OR m.document_number LIKE '%' || $3 || '%')
    AND ($4::uuid IS NULL OR m.home_branch_id = $4)
    AND ($5::uuid IS NULL OR m.id = $5)
)`

// pageQuery selects one page of base and then looks up branch, current period and last receipt
// for those rows only. $6 status filter (” = any), $7 limit, $8 offset.
const pageQuery = statusBase + `, page AS (
  SELECT * FROM base WHERE ($6::text = '' OR status = $6) ORDER BY created_at DESC, id DESC LIMIT $7 OFFSET $8
)
SELECT pg.id, pg.document_type, pg.document_number, pg.full_name, pg.phone, pg.created_at,
       b.id, b.name, COALESCE(g.display_name, g.name), pg.status, pg.days_remaining,
       COALESCE(cur.months, 0), cur.starts_on, cur.ends_on, lr.number
FROM page pg
JOIN branches b ON b.id = pg.home_branch_id AND b.organization_id = $1
JOIN gyms g ON g.id = b.gym_id AND g.organization_id = b.organization_id
LEFT JOIN LATERAL (
  SELECT months, starts_on, ends_on FROM memberships WHERE member_id = pg.id
  ORDER BY (starts_on <= $2::date AND ends_on >= $2::date) DESC, ends_on DESC LIMIT 1) cur ON true
LEFT JOIN LATERAL (
  SELECT r.number FROM payments p JOIN receipts r ON r.id = p.receipt_id
  WHERE p.member_id = pg.id ORDER BY p.created_at DESC, r.number DESC LIMIT 1) lr ON true
ORDER BY pg.created_at DESC, pg.id DESC`

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func scanMember(row pgx.Row) (app.MemberView, error) {
	var v app.MemberView
	var months int
	var starts, ends *time.Time
	var last *int
	err := row.Scan(&v.ID, &v.DocumentType, &v.DocumentNumber, &v.FullName, &v.Phone, new(time.Time),
		&v.HomeBranch.ID, &v.HomeBranch.Name, &v.HomeBranch.GymName, &v.Status, &v.DaysRemaining,
		&months, &starts, &ends, &last)
	if err != nil {
		return v, err
	}
	v.PlanLabel = domain.PlanLabel(months)
	if starts != nil {
		v.StartsOn = formatDate(*starts)
	}
	if ends != nil {
		v.EndsOn = formatDate(*ends)
	}
	if last != nil {
		label := receipts.Label(receipts.Membership, *last)
		v.LastReceipt = &label
	}
	return v, nil
}

// List returns a page of the shared directory and the counts per status.
func (r *Repository) List(ctx context.Context, organizationID uuid.UUID, f app.Filter, today time.Time) (app.ListResult, error) {
	query := likeEscaper.Replace(strings.TrimSpace(f.Query))
	var out app.ListResult
	err := r.pool.QueryRow(ctx, statusBase+`
		SELECT count(*), count(*) FILTER (WHERE status='active'), count(*) FILTER (WHERE status='expiring'),
		       count(*) FILTER (WHERE status='expired'), count(*) FILTER (WHERE status='scheduled') FROM base`,
		organizationID, today, query, f.HomeBranchID, nil).
		Scan(&out.Counts.All, &out.Counts.Active, &out.Counts.Expiring, &out.Counts.Expired, &out.Counts.Scheduled)
	if err != nil {
		return out, err
	}
	switch f.Status {
	case "":
		out.Total = out.Counts.All
	case "active":
		out.Total = out.Counts.Active
	case "expiring":
		out.Total = out.Counts.Expiring
	case "expired":
		out.Total = out.Counts.Expired
	case "scheduled":
		out.Total = out.Counts.Scheduled
	}
	rows, err := r.pool.Query(ctx, pageQuery, organizationID, today, query, f.HomeBranchID, nil, f.Status, f.PageSize, (f.Page-1)*f.PageSize)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Items = []app.MemberView{}
	for rows.Next() {
		v, err := scanMember(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	return out, rows.Err()
}

// Member returns a member with all periods.
func (r *Repository) Member(ctx context.Context, organizationID, memberID uuid.UUID, today time.Time) (app.MemberDetail, error) {
	view, err := scanMember(r.pool.QueryRow(ctx, pageQuery, organizationID, today, "", nil, memberID, "", 1, 0))
	if errors.Is(err, pgx.ErrNoRows) {
		return app.MemberDetail{}, domain.ErrMemberNotFound
	}
	if err != nil {
		return app.MemberDetail{}, err
	}
	detail := app.MemberDetail{MemberView: view, Memberships: []app.MembershipView{}}
	rows, err := r.pool.Query(ctx, `SELECT ms.id, ms.months, ms.starts_on, ms.ends_on, ms.amount::text, ms.access_scope, rc.id, rc.kind, rc.number
		FROM memberships ms
		LEFT JOIN payments p ON p.membership_id = ms.id
		LEFT JOIN receipts rc ON rc.id = p.receipt_id
		WHERE ms.member_id = $1 AND ms.organization_id = $2 ORDER BY ms.starts_on DESC, ms.created_at DESC`, memberID, organizationID)
	if err != nil {
		return detail, err
	}
	defer rows.Close()
	var periodIDs []uuid.UUID
	for rows.Next() {
		var m app.MembershipView
		var starts, ends time.Time
		var receiptID *uuid.UUID
		var kind *string
		var number *int
		if err := rows.Scan(&m.ID, &m.Months, &starts, &ends, &m.Amount, &m.AccessScope, &receiptID, &kind, &number); err != nil {
			return detail, err
		}
		m.PlanLabel, m.StartsOn, m.EndsOn = domain.PlanLabel(m.Months), formatDate(starts), formatDate(ends)
		m.AccessBranches = []app.AccessBranch{}
		if receiptID != nil {
			m.Receipt = &app.ReceiptRef{ID: *receiptID, Number: receipts.Label(receipts.Kind(*kind), *number)}
		}
		detail.Memberships = append(detail.Memberships, m)
		periodIDs = append(periodIDs, m.ID)
	}
	if err := rows.Err(); err != nil {
		return detail, err
	}
	rows.Close()
	labels, err := r.accessBranches(ctx, periodIDs)
	if err != nil {
		return detail, err
	}
	for i := range detail.Memberships {
		if l := labels[detail.Memberships[i].ID]; l != nil {
			detail.Memberships[i].AccessBranches = l
		}
	}
	return detail, nil
}

func (r *Repository) accessBranches(ctx context.Context, periodIDs []uuid.UUID) (map[uuid.UUID][]app.AccessBranch, error) {
	out := map[uuid.UUID][]app.AccessBranch{}
	if len(periodIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT mb.membership_id, b.id, COALESCE(g.display_name, g.name) || ' · ' || b.name
		FROM membership_branches mb
		JOIN branches b ON b.id = mb.branch_id AND b.organization_id = mb.organization_id
		JOIN gyms g ON g.id = b.gym_id AND g.organization_id = b.organization_id
		WHERE mb.membership_id = ANY($1) ORDER BY g.name, b.name`, periodIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var period uuid.UUID
		var branch app.AccessBranch
		if err := rows.Scan(&period, &branch.ID, &branch.Label); err != nil {
			return nil, err
		}
		out[period] = append(out[period], branch)
	}
	return out, rows.Err()
}

// UpdateMember changes name and phone.
func (r *Repository) UpdateMember(ctx context.Context, organizationID, memberID uuid.UUID, name, phone string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE members SET full_name=$3, phone=$4, updated_at=now() WHERE id=$1 AND organization_id=$2`,
		memberID, organizationID, name, phone)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

func insertPeriod(ctx context.Context, t pgx.Tx, org, memberID uuid.UUID, p app.Period) error {
	if _, err := t.Exec(ctx, `INSERT INTO memberships (id, organization_id, member_id, months, starts_on, ends_on, amount, access_scope)
		VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8)`, p.ID, org, memberID, p.Months, p.StartsOn, p.EndsOn, p.Amount, string(p.Scope)); err != nil {
		return err
	}
	for _, branch := range p.BranchIDs {
		if _, err := t.Exec(ctx, `INSERT INTO membership_branches (membership_id, branch_id, organization_id) VALUES ($1,$2,$3)`, p.ID, branch, org); err != nil {
			return err
		}
	}
	return nil
}

func sell(ctx context.Context, t pgx.Tx, org, actor, memberID, branch uuid.UUID, p app.Period, method string, today time.Time) error {
	receiptID, _, err := receipts.Issue(ctx, t, org, receipts.Membership, branch, today)
	if err != nil {
		return err
	}
	return receipts.Record(ctx, t, receipts.Payment{
		OrganizationID: org, ReceiptID: receiptID, BranchID: branch, Kind: receipts.Membership, Method: method,
		Amount: p.Amount, PaidOn: today, MemberID: &memberID, MembershipID: &p.ID, CreatedBy: actor,
	})
}

func constraintOf(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}

// Enroll stores member, period, receipt and payment in one transaction.
func (r *Repository) Enroll(ctx context.Context, e app.Enrollment) (app.SaleIDs, bool, error) {
	ids := app.SaleIDs{MemberID: e.MemberID, MembershipID: e.Period.ID}
	existed := false
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		err := t.QueryRow(ctx, `SELECT ms.id FROM members m JOIN memberships ms ON ms.member_id = m.id
			WHERE m.id = $1 AND m.organization_id = $2 ORDER BY ms.created_at LIMIT 1`, e.MemberID, e.OrganizationID).Scan(&ids.MembershipID)
		if err == nil {
			existed = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := t.Exec(ctx, `INSERT INTO members (id, organization_id, home_branch_id, document_type, document_number, full_name, phone)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`, e.MemberID, e.OrganizationID, e.HomeBranchID, e.DocumentType, e.DocumentNumber, e.FullName, e.Phone); err != nil {
			return err
		}
		if err := insertPeriod(ctx, t, e.OrganizationID, e.MemberID, e.Period); err != nil {
			return err
		}
		return sell(ctx, t, e.OrganizationID, e.ActorID, e.MemberID, e.HomeBranchID, e.Period, e.Method, e.Today)
	})
	switch constraintOf(err) {
	case "members_document_key":
		var existing uuid.UUID
		if lookup := r.pool.QueryRow(ctx, `SELECT id FROM members WHERE organization_id=$1 AND document_number=$2`, e.OrganizationID, e.DocumentNumber).Scan(&existing); lookup != nil {
			return ids, false, domain.ErrConflict
		}
		return ids, false, &domain.DocumentTakenError{MemberID: existing}
	case "members_pkey", "memberships_pkey":
		return ids, false, domain.ErrConflict
	}
	return ids, existed, err
}

// Renew locks the member, lets the service decide the period and sells it.
func (r *Repository) Renew(ctx context.Context, rn app.Renewal, plan app.PlanFunc) (app.SaleIDs, bool, error) {
	ids := app.SaleIDs{MemberID: rn.MemberID}
	existed := false
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		var home uuid.UUID
		err := t.QueryRow(ctx, `SELECT home_branch_id FROM members WHERE id=$1 AND organization_id=$2 FOR UPDATE`, rn.MemberID, rn.OrganizationID).Scan(&home)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrMemberNotFound
		}
		if err != nil {
			return err
		}
		if rn.PeriodID != uuid.Nil {
			err := t.QueryRow(ctx, `SELECT id FROM memberships WHERE id=$1 AND organization_id=$2 AND member_id=$3`, rn.PeriodID, rn.OrganizationID, rn.MemberID).Scan(&ids.MembershipID)
			if err == nil {
				existed = true
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		var latest *time.Time
		if err := t.QueryRow(ctx, `SELECT max(ends_on) FROM memberships WHERE member_id=$1`, rn.MemberID).Scan(&latest); err != nil {
			return err
		}
		period, err := plan(home, latest)
		if err != nil {
			return err
		}
		ids.MembershipID = period.ID
		if err := insertPeriod(ctx, t, rn.OrganizationID, rn.MemberID, period); err != nil {
			return err
		}
		return sell(ctx, t, rn.OrganizationID, rn.ActorID, rn.MemberID, rn.CollectedBranchID, period, rn.Method, rn.Today)
	})
	if constraintOf(err) == "memberships_pkey" {
		return ids, false, domain.ErrConflict
	}
	return ids, existed, err
}

// MemberReceipts lists the receipts of a member, newest first.
func (r *Repository) MemberReceipts(ctx context.Context, organizationID, memberID uuid.UUID) ([]app.ReceiptSummary, error) {
	rows, err := r.pool.Query(ctx, `SELECT rc.id, rc.kind, rc.number, rc.issued_on, COALESCE(ms.months, 0), p.amount::text, p.method
		FROM payments p JOIN receipts rc ON rc.id = p.receipt_id
		LEFT JOIN memberships ms ON ms.id = p.membership_id
		WHERE p.member_id = $1 AND p.organization_id = $2 ORDER BY rc.issued_on DESC, rc.number DESC`, memberID, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.ReceiptSummary{}
	for rows.Next() {
		var s app.ReceiptSummary
		var kind string
		var number, months int
		var issued time.Time
		if err := rows.Scan(&s.ID, &kind, &number, &issued, &months, &s.Amount, &s.Method); err != nil {
			return nil, err
		}
		s.Number, s.IssuedOn, s.PlanLabel = receipts.Label(receipts.Kind(kind), number), formatDate(issued), domain.PlanLabel(months)
		out = append(out, s)
	}
	return out, rows.Err()
}

// Receipt returns the printable data of one receipt.
func (r *Repository) Receipt(ctx context.Context, organizationID, receiptID uuid.UUID) (app.ReceiptDetail, error) {
	var d app.ReceiptDetail
	var kind string
	var number int
	var issued time.Time
	var memberID, membershipID, dayPassID *uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT r.id, r.kind, r.number, r.issued_on, COALESCE(g.display_name, g.name), g.tax_id, g.brand_color,
			b.name, b.address, p.amount::text, p.method, p.member_id, p.membership_id, p.day_pass_id
		FROM receipts r JOIN payments p ON p.receipt_id = r.id AND p.organization_id = r.organization_id
		JOIN branches b ON b.id = r.branch_id AND b.organization_id = r.organization_id
		JOIN gyms g ON g.id = b.gym_id AND g.organization_id = b.organization_id
		WHERE r.id = $1 AND r.organization_id = $2`, receiptID, organizationID).
		Scan(&d.ID, &kind, &number, &issued, &d.Gym.Name, &d.Gym.TaxID, &d.Gym.BrandColor, &d.Branch.Name, &d.Branch.Address,
			&d.Payment.Amount, &d.Payment.Method, &memberID, &membershipID, &dayPassID)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, domain.ErrReceiptNotFound
	}
	if err != nil {
		return d, err
	}
	d.Kind, d.Number, d.IssuedOn = kind, receipts.Label(receipts.Kind(kind), number), formatDate(issued)
	if memberID != nil {
		var m app.ReceiptMember
		if err := r.pool.QueryRow(ctx, `SELECT full_name, document_type, document_number, phone FROM members WHERE id=$1 AND organization_id=$2`,
			*memberID, organizationID).Scan(&m.FullName, &m.DocumentType, &m.DocumentNumber, &m.Phone); err != nil {
			return d, err
		}
		d.Member = &m
	}
	if dayPassID != nil {
		// The optional customer of a day pass is shown like a member, when there is anything to show.
		var kind, number, name, phone *string
		if err := r.pool.QueryRow(ctx, `SELECT customer_document_type, customer_document_number, customer_name, customer_phone FROM day_passes WHERE id=$1 AND organization_id=$2`,
			*dayPassID, organizationID).Scan(&kind, &number, &name, &phone); err != nil {
			return d, err
		}
		if name != nil || number != nil || phone != nil {
			visitor := app.ReceiptMember{FullName: "Visitante"}
			if name != nil {
				visitor.FullName = *name
			}
			if kind != nil && number != nil {
				visitor.DocumentType, visitor.DocumentNumber = *kind, *number
			}
			if phone != nil {
				visitor.Phone = *phone
			}
			d.Member = &visitor
		}
	}
	if membershipID != nil {
		var m app.ReceiptMembership
		var starts, ends time.Time
		if err := r.pool.QueryRow(ctx, `SELECT months, starts_on, ends_on FROM memberships WHERE id=$1 AND organization_id=$2`,
			*membershipID, organizationID).Scan(&m.Months, &starts, &ends); err != nil {
			return d, err
		}
		m.PlanLabel, m.StartsOn, m.EndsOn = domain.PlanLabel(m.Months), formatDate(starts), formatDate(ends)
		labels, err := r.accessBranches(ctx, []uuid.UUID{*membershipID})
		if err != nil {
			return d, err
		}
		m.AccessBranches = labels[*membershipID]
		if m.AccessBranches == nil {
			m.AccessBranches = []app.AccessBranch{}
		}
		d.Membership = &m
	}
	return d, nil
}
