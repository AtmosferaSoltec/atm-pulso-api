// Package postgres is the persistence adapter of members.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/memberstatus"
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
// $1 gym, $2 today (Lima), $3 search text, $4 home branch (or NULL), $5 member id (or NULL).
var statusBase = memberstatus.Prefix("$1", "$2::date") + `, base AS (
  SELECT m.id, m.document_type, m.document_number, m.full_name, m.phone, m.created_at, m.home_branch_id,
         CASE WHEN cv.end_on IS NOT NULL THEN
                CASE WHEN cv.end_on - $2::date <= 7 THEN 'expiring' ELSE 'active' END
              WHEN COALESCE(s.scheduled, false) THEN 'scheduled'
              ELSE 'expired' END AS status,
         COALESCE(COALESCE(cv.end_on, s.latest_end) - $2::date, 0) AS days_remaining
  FROM members m
  LEFT JOIN coverage cv ON cv.member_id = m.id
  LEFT JOIN LATERAL (
    SELECT max(ends_on) AS latest_end, bool_or(starts_on > $2::date) AS scheduled
    FROM memberships WHERE member_id = m.id) s ON true
  WHERE m.gym_id = $1
    AND ($3::text = '' OR m.full_name ILIKE '%' || $3 || '%' OR m.document_number LIKE '%' || $3 || '%')
    AND ($4::uuid IS NULL OR m.home_branch_id = $4)
    AND ($5::uuid IS NULL OR m.id = $5)
)`

// pageQuery selects one page of base and then looks up branch, current period and last receipt
// for those rows only. $6 status filter (” = any), $7 limit, $8 offset.
var pageQuery = statusBase + `, page AS (
  SELECT * FROM base WHERE ($6::text = '' OR status = $6) ORDER BY created_at DESC, id DESC LIMIT $7 OFFSET $8
)
SELECT pg.id, pg.document_type, pg.document_number, pg.full_name, pg.phone, pg.created_at,
       b.id, b.name, COALESCE(g.display_name, g.name), pg.status, pg.days_remaining,
       COALESCE(cur.months, 0), cur.starts_on, cur.ends_on, lr.number, cur.plan_name, names.current
FROM page pg
JOIN branches b ON b.id = pg.home_branch_id AND b.gym_id = $1
JOIN gyms g ON g.id = b.gym_id
LEFT JOIN LATERAL (
  SELECT months, starts_on, ends_on, plan_name FROM memberships WHERE member_id = pg.id
  ORDER BY (starts_on <= $2::date AND ends_on >= $2::date) DESC, ends_on DESC LIMIT 1) cur ON true
LEFT JOIN LATERAL (
  SELECT COALESCE(array_agg(DISTINCT plan_name ORDER BY plan_name), '{}') AS current FROM memberships
  WHERE member_id = pg.id AND starts_on <= $2::date AND ends_on >= $2::date) names ON true
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
	var lastPlan *string
	var current []string
	err := row.Scan(&v.ID, &v.DocumentType, &v.DocumentNumber, &v.FullName, &v.Phone, new(time.Time),
		&v.HomeBranch.ID, &v.HomeBranch.Name, &v.HomeBranch.GymName, &v.Status, &v.DaysRemaining,
		&months, &starts, &ends, &last, &lastPlan, &current)
	if err != nil {
		return v, err
	}
	// The plans held today; for a member without a current period, the last plan they held.
	v.PlanNames = current
	if len(v.PlanNames) == 0 {
		v.PlanNames = []string{}
		if lastPlan != nil {
			v.PlanNames = []string{*lastPlan}
		}
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
func (r *Repository) List(ctx context.Context, gymID uuid.UUID, f app.Filter, today time.Time) (app.ListResult, error) {
	query := likeEscaper.Replace(strings.TrimSpace(f.Query))
	var out app.ListResult
	err := r.pool.QueryRow(ctx, statusBase+`
		SELECT count(*), count(*) FILTER (WHERE status='active'), count(*) FILTER (WHERE status='expiring'),
		       count(*) FILTER (WHERE status='expired'), count(*) FILTER (WHERE status='scheduled') FROM base`,
		gymID, today, query, f.HomeBranchID, nil).
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
	rows, err := r.pool.Query(ctx, pageQuery, gymID, today, query, f.HomeBranchID, nil, f.Status, f.PageSize, (f.Page-1)*f.PageSize)
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
func (r *Repository) Member(ctx context.Context, gymID, memberID uuid.UUID, today time.Time) (app.MemberDetail, error) {
	view, err := scanMember(r.pool.QueryRow(ctx, pageQuery, gymID, today, "", nil, memberID, "", 1, 0))
	if errors.Is(err, pgx.ErrNoRows) {
		return app.MemberDetail{}, domain.ErrMemberNotFound
	}
	if err != nil {
		return app.MemberDetail{}, err
	}
	detail := app.MemberDetail{MemberView: view, Memberships: []app.MembershipView{}}
	rows, err := r.pool.Query(ctx, `SELECT ms.id, ms.plan_id, ms.plan_name, ms.months, ms.starts_on, ms.ends_on, ms.amount::text, ms.access_scope, ms.amended_at, rc.id, rc.kind, rc.number
		FROM memberships ms
		LEFT JOIN payments p ON p.membership_id = ms.id
		LEFT JOIN receipts rc ON rc.id = p.receipt_id
		WHERE ms.member_id = $1 AND ms.gym_id = $2 ORDER BY ms.starts_on DESC, ms.created_at DESC`, memberID, gymID)
	if err != nil {
		return detail, err
	}
	defer rows.Close()
	var periodIDs []uuid.UUID
	for rows.Next() {
		var m app.MembershipView
		var starts, ends time.Time
		var amended *time.Time
		var receiptID *uuid.UUID
		var kind *string
		var number *int
		if err := rows.Scan(&m.ID, &m.PlanID, &m.PlanName, &m.Months, &starts, &ends, &m.Amount, &m.AccessScope, &amended, &receiptID, &kind, &number); err != nil {
			return detail, err
		}
		if amended != nil {
			at := amended.UTC().Format(time.RFC3339)
			m.AmendedAt = &at
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
		JOIN branches b ON b.id = mb.branch_id AND b.gym_id = mb.gym_id
		JOIN gyms g ON g.id = b.gym_id
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
func (r *Repository) UpdateMember(ctx context.Context, gymID, memberID uuid.UUID, name, phone string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE members SET full_name=$3, phone=$4, updated_at=now() WHERE id=$1 AND gym_id=$2`,
		memberID, gymID, name, phone)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMemberNotFound
	}
	return nil
}

func insertPeriod(ctx context.Context, t pgx.Tx, gymID, memberID uuid.UUID, p app.Period) error {
	if _, err := t.Exec(ctx, `INSERT INTO memberships (id, gym_id, member_id, plan_id, plan_name, months, starts_on, ends_on, amount, access_scope)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::numeric,$10)`, p.ID, gymID, memberID, p.PlanID, p.PlanName, p.Months, p.StartsOn, p.EndsOn, p.Amount, string(p.Scope)); err != nil {
		return err
	}
	for _, branch := range p.BranchIDs {
		if _, err := t.Exec(ctx, `INSERT INTO membership_branches (membership_id, branch_id, gym_id) VALUES ($1,$2,$3)`, p.ID, branch, gymID); err != nil {
			return err
		}
	}
	return nil
}

func sell(ctx context.Context, t pgx.Tx, gymID, actor, memberID, branch uuid.UUID, p app.Period, method string, today time.Time) error {
	receiptID, _, err := receipts.Issue(ctx, t, gymID, receipts.Membership, branch, today)
	if err != nil {
		return err
	}
	return receipts.Record(ctx, t, receipts.Payment{
		GymID: gymID, ReceiptID: receiptID, BranchID: branch, Kind: receipts.Membership, Method: method,
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
			WHERE m.id = $1 AND m.gym_id = $2 ORDER BY ms.created_at LIMIT 1`, e.MemberID, e.GymID).Scan(&ids.MembershipID)
		if err == nil {
			existed = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		planID, planName, err := resolvePlan(ctx, t, e.GymID, e.PlanID)
		if err != nil {
			return err
		}
		e.Period.PlanID, e.Period.PlanName = planID, planName
		if _, err := t.Exec(ctx, `INSERT INTO members (id, gym_id, home_branch_id, document_type, document_number, full_name, phone)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`, e.MemberID, e.GymID, e.HomeBranchID, e.DocumentType, e.DocumentNumber, e.FullName, e.Phone); err != nil {
			return err
		}
		if err := insertPeriod(ctx, t, e.GymID, e.MemberID, e.Period); err != nil {
			return err
		}
		return sell(ctx, t, e.GymID, e.ActorID, e.MemberID, e.HomeBranchID, e.Period, e.Method, e.Today)
	})
	switch constraintOf(err) {
	case "members_document_key":
		var existing uuid.UUID
		if lookup := r.pool.QueryRow(ctx, `SELECT id FROM members WHERE gym_id=$1 AND document_number=$2`, e.GymID, e.DocumentNumber).Scan(&existing); lookup != nil {
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
		err := t.QueryRow(ctx, `SELECT home_branch_id FROM members WHERE id=$1 AND gym_id=$2 FOR UPDATE`, rn.MemberID, rn.GymID).Scan(&home)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrMemberNotFound
		}
		if err != nil {
			return err
		}
		if rn.PeriodID != uuid.Nil {
			err := t.QueryRow(ctx, `SELECT id FROM memberships WHERE id=$1 AND gym_id=$2 AND member_id=$3`, rn.PeriodID, rn.GymID, rn.MemberID).Scan(&ids.MembershipID)
			if err == nil {
				existed = true
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		planID, planName, err := resolvePlan(ctx, t, rn.GymID, rn.PlanID)
		if err != nil {
			return err
		}
		// A renewal continues the same plan: its default start is where the member's last period of THAT
		// plan ends, whatever the other plans of the member are doing.
		var latest *time.Time
		if err := t.QueryRow(ctx, `SELECT max(ends_on) FROM memberships WHERE member_id=$1 AND plan_id=$2`, rn.MemberID, planID).Scan(&latest); err != nil {
			return err
		}
		period, err := plan(home, latest)
		if err != nil {
			return err
		}
		period.PlanID, period.PlanName = planID, planName
		if err := checkOverlap(ctx, t, rn.GymID, rn.MemberID, planID, uuid.Nil, period.StartsOn, period.EndsOn); err != nil {
			return err
		}
		ids.MembershipID = period.ID
		if err := insertPeriod(ctx, t, rn.GymID, rn.MemberID, period); err != nil {
			return err
		}
		return sell(ctx, t, rn.GymID, rn.ActorID, rn.MemberID, rn.CollectedBranchID, period, rn.Method, rn.Today)
	})
	if constraintOf(err) == "memberships_pkey" {
		return ids, false, domain.ErrConflict
	}
	return ids, existed, err
}

// MemberReceipts lists the receipts of a member, newest first.
func (r *Repository) MemberReceipts(ctx context.Context, gymID, memberID uuid.UUID) ([]app.ReceiptSummary, error) {
	rows, err := r.pool.Query(ctx, `SELECT rc.id, rc.kind, rc.number, rc.issued_on, COALESCE(ms.months, 0), ms.plan_name, p.amount::text, p.method
		FROM payments p JOIN receipts rc ON rc.id = p.receipt_id
		LEFT JOIN memberships ms ON ms.id = p.membership_id
		WHERE p.member_id = $1 AND p.gym_id = $2 ORDER BY rc.issued_on DESC, rc.number DESC`, memberID, gymID)
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
		if err := rows.Scan(&s.ID, &kind, &number, &issued, &months, &s.PlanName, &s.Amount, &s.Method); err != nil {
			return nil, err
		}
		s.Number, s.IssuedOn, s.PlanLabel = receipts.Label(receipts.Kind(kind), number), formatDate(issued), domain.PlanLabel(months)
		out = append(out, s)
	}
	return out, rows.Err()
}

// Receipt returns the printable data of one receipt.
func (r *Repository) Receipt(ctx context.Context, gymID, receiptID uuid.UUID) (app.ReceiptDetail, error) {
	var d app.ReceiptDetail
	var kind string
	var number int
	var issued time.Time
	var memberID, membershipID, dayPassID *uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT r.id, r.kind, r.number, r.issued_on, g.id, COALESCE(g.display_name, g.name), g.tax_id, g.brand_color,
			b.name, b.address, p.amount::text, p.method, p.member_id, p.membership_id, p.day_pass_id
		FROM receipts r JOIN payments p ON p.receipt_id = r.id AND p.gym_id = r.gym_id
		JOIN branches b ON b.id = r.branch_id AND b.gym_id = r.gym_id
		JOIN gyms g ON g.id = b.gym_id
		WHERE r.id = $1 AND r.gym_id = $2`, receiptID, gymID).
		Scan(&d.ID, &kind, &number, &issued, &d.Gym.ID, &d.Gym.Name, &d.Gym.TaxID, &d.Gym.BrandColor, &d.Branch.Name, &d.Branch.Address,
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
		if err := r.pool.QueryRow(ctx, `SELECT full_name, document_type, document_number, phone FROM members WHERE id=$1 AND gym_id=$2`,
			*memberID, gymID).Scan(&m.FullName, &m.DocumentType, &m.DocumentNumber, &m.Phone); err != nil {
			return d, err
		}
		d.Member = &m
	}
	if dayPassID != nil {
		// The optional customer of a day pass is shown like a member, when there is anything to show.
		var kind, number, name, phone *string
		if err := r.pool.QueryRow(ctx, `SELECT customer_document_type, customer_document_number, customer_name, customer_phone FROM day_passes WHERE id=$1 AND gym_id=$2`,
			*dayPassID, gymID).Scan(&kind, &number, &name, &phone); err != nil {
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
		if err := r.pool.QueryRow(ctx, `SELECT months, starts_on, ends_on, plan_name FROM memberships WHERE id=$1 AND gym_id=$2`,
			*membershipID, gymID).Scan(&m.Months, &starts, &ends, &m.PlanName); err != nil {
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

// checkOverlap returns *domain.OverlapError when the member already holds, in the same plan, a period (other than
// except) that collides with [starts, ends). The caller holds the member row lock, so two concurrent sales of the
// same plan cannot both pass.
func checkOverlap(ctx context.Context, t pgx.Tx, gymID, memberID, planID, except uuid.UUID, starts, ends time.Time) error {
	var clash domain.OverlapError
	err := t.QueryRow(ctx, `SELECT id, starts_on, ends_on FROM memberships
		WHERE gym_id=$1 AND member_id=$2 AND plan_id=$3 AND id <> $4 AND starts_on < $6 AND ends_on > $5
		ORDER BY starts_on LIMIT 1`, gymID, memberID, planID, except, starts, ends).Scan(&clash.MembershipID, &clash.StartsOn, &clash.EndsOn)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := t.QueryRow(ctx, `SELECT max(ends_on) FROM memberships WHERE gym_id=$1 AND member_id=$2 AND plan_id=$3 AND id <> $4`,
		gymID, memberID, planID, except).Scan(&clash.SuggestedStart); err != nil {
		return err
	}
	return &clash
}

// periodState is the JSON stored in membership_amendments.before / after.
func periodState(months int, starts, ends time.Time, amount, method string) map[string]any {
	state := map[string]any{"months": months, "starts_on": formatDate(starts), "ends_on": formatDate(ends), "amount": amount}
	if method != "" {
		state["method"] = method
	}
	return state
}

// AmendMembership corrects a period in place and records who did it. See app.Repository.
func (r *Repository) AmendMembership(ctx context.Context, gymID, actorID, memberID, membershipID uuid.UUID, decide app.AmendFunc) (bool, error) {
	changed := false
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		var locked uuid.UUID
		err := t.QueryRow(ctx, `SELECT id FROM members WHERE id=$1 AND gym_id=$2 FOR UPDATE`, memberID, gymID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrMemberNotFound
		}
		if err != nil {
			return err
		}
		var cur app.CurrentPeriod
		err = t.QueryRow(ctx, `SELECT ms.plan_id, ms.months, ms.starts_on, ms.ends_on, ms.amount::text, p.id, COALESCE(p.method, ''), p.branch_id
			FROM memberships ms LEFT JOIN payments p ON p.membership_id = ms.id AND p.gym_id = ms.gym_id
			WHERE ms.id=$1 AND ms.member_id=$2 AND ms.gym_id=$3`, membershipID, memberID, gymID).
			Scan(&cur.PlanID, &cur.Months, &cur.StartsOn, &cur.EndsOn, &cur.Amount, &cur.PaymentID, &cur.Method, &cur.PaymentBranchID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrMembershipNotFound
		}
		if err != nil {
			return err
		}
		change, err := decide(cur)
		if err != nil {
			return err
		}
		if change.Months == cur.Months && change.StartsOn.Equal(cur.StartsOn) && change.EndsOn.Equal(cur.EndsOn) &&
			change.Amount == cur.Amount && change.Method == cur.Method {
			return nil
		}
		if err := checkOverlap(ctx, t, gymID, memberID, cur.PlanID, membershipID, change.StartsOn, change.EndsOn); err != nil {
			return err
		}
		if _, err := t.Exec(ctx, `UPDATE memberships SET months=$3, starts_on=$4, ends_on=$5, amount=$6::numeric, amended_at=now()
			WHERE id=$1 AND gym_id=$2`, membershipID, gymID, change.Months, change.StartsOn, change.EndsOn, change.Amount); err != nil {
			return err
		}
		if cur.PaymentID != nil {
			if _, err := t.Exec(ctx, `UPDATE payments SET amount=$3::numeric, method=$4 WHERE id=$1 AND gym_id=$2`,
				*cur.PaymentID, gymID, change.Amount, change.Method); err != nil {
				return err
			}
		}
		before := periodState(cur.Months, cur.StartsOn, cur.EndsOn, cur.Amount, cur.Method)
		after := periodState(change.Months, change.StartsOn, change.EndsOn, change.Amount, change.Method)
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if _, err := t.Exec(ctx, `INSERT INTO membership_amendments (id, gym_id, membership_id, payment_id, changed_by, before, after)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`, ids.New(), gymID, membershipID, cur.PaymentID, actorID, beforeJSON, afterJSON); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

// Amendments lists the corrections of a period, oldest first.
func (r *Repository) Amendments(ctx context.Context, gymID, memberID, membershipID uuid.UUID) ([]app.AmendmentView, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM memberships WHERE id=$1 AND member_id=$2 AND gym_id=$3)`,
		membershipID, memberID, gymID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrMembershipNotFound
	}
	rows, err := r.pool.Query(ctx, `SELECT a.id, a.changed_at, a.changed_by, u.full_name, a.before, a.after
		FROM membership_amendments a JOIN users u ON u.id = a.changed_by
		WHERE a.membership_id=$1 AND a.gym_id=$2 ORDER BY a.changed_at, a.id`, membershipID, gymID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.AmendmentView{}
	for rows.Next() {
		var v app.AmendmentView
		var at time.Time
		if err := rows.Scan(&v.ID, &at, &v.ChangedBy.ID, &v.ChangedBy.FullName, &v.Before, &v.After); err != nil {
			return nil, err
		}
		v.ChangedAt = at.UTC().Format(time.RFC3339)
		out = append(out, v)
	}
	return out, rows.Err()
}
