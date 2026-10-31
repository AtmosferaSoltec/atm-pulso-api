// Package postgres is the persistence adapter of products.
package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

// Repository stores the catalog, sales, pending sales and collections.
type Repository struct{ pool *pgxpool.Pool }

// NewRepository builds the repository.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func constraintOf(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}

// ---- Catalog ----

// Products lists the catalog by position.
func (r *Repository) Products(ctx context.Context, gymID uuid.UUID, includeInactive bool) ([]app.ProductView, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, price::text, is_active, position FROM products
		WHERE gym_id = $1 AND ($2 OR is_active) ORDER BY position, lower(name), id`, gymID, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.ProductView{}
	for rows.Next() {
		var v app.ProductView
		if err := rows.Scan(&v.ID, &v.Name, &v.Price, &v.IsActive, &v.Position); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func oneProduct(ctx context.Context, q querier, gymID, id uuid.UUID) (app.ProductView, error) {
	var v app.ProductView
	err := q.QueryRow(ctx, `SELECT id, name, price::text, is_active, position FROM products WHERE gym_id = $1 AND id = $2`, gymID, id).
		Scan(&v.ID, &v.Name, &v.Price, &v.IsActive, &v.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, domain.ErrProductNotFound
	}
	return v, err
}

func translateProductWrite(err error) error {
	switch constraintOf(err) {
	case "products_name_key":
		return domain.ErrProductNameTaken
	case "products_pkey", "products_id_gym_key":
		return domain.ErrConflict
	}
	return err
}

// CreateProduct stores a product; a repeated id of the same gym answers the stored one.
func (r *Repository) CreateProduct(ctx context.Context, gymID uuid.UUID, p app.NewProduct) (app.ProductView, bool, error) {
	if existing, err := oneProduct(ctx, r.pool, gymID, p.ID); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, domain.ErrProductNotFound) {
		return app.ProductView{}, false, err
	}
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		if _, err := t.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 1))`, gymID.String()); err != nil {
			return err
		}
		_, err := t.Exec(ctx, `INSERT INTO products (id, gym_id, name, price, is_active, position)
			VALUES ($1, $2, $3, $4::numeric, $5, CASE WHEN $6 < 0 THEN COALESCE((SELECT max(position) + 1 FROM products WHERE gym_id = $2), 0) ELSE $6 END)`,
			p.ID, gymID, p.Name, p.Price, p.IsActive, p.Position)
		return err
	})
	if err != nil {
		return app.ProductView{}, false, translateProductWrite(err)
	}
	v, err := oneProduct(ctx, r.pool, gymID, p.ID)
	return v, false, err
}

// UpdateProduct applies the fields sent.
func (r *Repository) UpdateProduct(ctx context.Context, gymID, id uuid.UUID, c app.ProductPatch) (app.ProductView, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE products SET
		name = COALESCE($3, name), price = COALESCE($4::numeric, price), is_active = COALESCE($5, is_active),
		position = COALESCE($6, position), updated_at = now() WHERE id = $1 AND gym_id = $2`,
		id, gymID, c.Name, c.Price, c.IsActive, c.Position)
	if err != nil {
		return app.ProductView{}, translateProductWrite(err)
	}
	if tag.RowsAffected() == 0 {
		return app.ProductView{}, domain.ErrProductNotFound
	}
	return oneProduct(ctx, r.pool, gymID, id)
}

// ---- Sales ----

const saleColumns = `s.id, s.branch_id, s.sold_by, s.member_id, s.status, s.was_pending, s.method, s.total::text, s.units,
	s.sold_at, s.sold_on, s.collection_id, st.collected_at`
const saleFrom = ` FROM sales s LEFT JOIN sale_collections st ON st.id = s.collection_id AND st.gym_id = s.gym_id`

// loadSales reads sales (and their items) with a WHERE clause on alias s; $1 is always the gym.
func loadSales(ctx context.Context, q querier, where, order string, limitOffset string, args ...any) ([]app.SaleRecord, error) {
	rows, err := q.Query(ctx, `SELECT `+saleColumns+saleFrom+` WHERE s.gym_id = $1 `+where+` ORDER BY `+order+` `+limitOffset, args...)
	if err != nil {
		return nil, err
	}
	var recs []app.SaleRecord
	index := map[uuid.UUID]int{}
	var saleIDs []uuid.UUID
	for rows.Next() {
		var rec app.SaleRecord
		if err := rows.Scan(&rec.ID, &rec.BranchID, &rec.SoldBy, &rec.MemberID, &rec.Status, &rec.WasPending, &rec.Method, &rec.Total, &rec.Units,
			&rec.SoldAt, &rec.SoldOn, &rec.CollectionID, &rec.CollectedAt); err != nil {
			rows.Close()
			return nil, err
		}
		rec.Items = []app.SaleItemView{}
		index[rec.ID] = len(recs)
		saleIDs = append(saleIDs, rec.ID)
		recs = append(recs, rec)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return recs, nil
	}
	items, err := q.Query(ctx, `SELECT sale_id, product_id, product_name, quantity, unit_price::text, (quantity * unit_price)::text
		FROM sale_items WHERE gym_id = $1 AND sale_id = ANY($2) ORDER BY product_name, id`, args[0], saleIDs)
	if err != nil {
		return nil, err
	}
	defer items.Close()
	for items.Next() {
		var saleID uuid.UUID
		var it app.SaleItemView
		if err := items.Scan(&saleID, &it.ProductID, &it.Name, &it.Quantity, &it.UnitPrice, &it.LineTotal); err != nil {
			return nil, err
		}
		recs[index[saleID]].Items = append(recs[index[saleID]].Items, it)
	}
	return recs, items.Err()
}

func oneSale(ctx context.Context, q querier, gymID, id uuid.UUID) (app.SaleRecord, error) {
	recs, err := loadSales(ctx, q, `AND s.id = $2`, `s.id`, ``, gymID, id)
	if err != nil {
		return app.SaleRecord{}, err
	}
	if len(recs) == 0 {
		return app.SaleRecord{}, domain.ErrSaleNotFound
	}
	return recs[0], nil
}

// CreateSale stores the sale and its items; prices are read from the catalog inside the transaction.
func (r *Repository) CreateSale(ctx context.Context, s app.NewSale) (app.SaleRecord, bool, error) {
	var rec app.SaleRecord
	existed := false
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		if found, err := oneSale(ctx, t, s.GymID, s.ID); err == nil {
			rec, existed = found, true
			return nil
		} else if !errors.Is(err, domain.ErrSaleNotFound) {
			return err
		}
		productIDs := make([]uuid.UUID, 0, len(s.Lines))
		for _, l := range s.Lines {
			productIDs = append(productIDs, l.ProductID)
		}
		rows, err := t.Query(ctx, `SELECT id, name, price::text FROM products WHERE gym_id = $1 AND id = ANY($2) AND is_active ORDER BY id FOR SHARE`, s.GymID, productIDs)
		if err != nil {
			return err
		}
		type priced struct {
			name  string
			cents int64
			price string
		}
		catalog := map[uuid.UUID]priced{}
		for rows.Next() {
			var id uuid.UUID
			var p priced
			if err := rows.Scan(&id, &p.name, &p.price); err != nil {
				rows.Close()
				return err
			}
			p.cents, _ = domain.ParseMoney(p.price)
			catalog[id] = p
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var total int64
		units := 0
		for _, l := range s.Lines {
			p, ok := catalog[l.ProductID]
			if !ok {
				return &domain.ValidationError{Issues: []domain.Issue{{Field: "items", Code: domain.CodeProduct}}}
			}
			total += p.cents * int64(l.Quantity)
			units += l.Quantity
		}
		if total > domain.MaxSaleCents {
			return &domain.ValidationError{Issues: []domain.Issue{{Field: "items", Code: domain.CodeTotal}}}
		}
		was_pending := s.Status == domain.StatusPending
		var method *string
		if !was_pending {
			method = &s.Method
		}
		if _, err := t.Exec(ctx, `INSERT INTO sales (id, gym_id, branch_id, sold_by, member_id, status, was_pending, method, total, units, sold_on)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::numeric,$10,$11)`,
			s.ID, s.GymID, s.BranchID, s.SoldBy, s.MemberID, s.Status, was_pending, method, domain.FormatMoney(total), units, s.Today); err != nil {
			return err
		}
		for _, l := range s.Lines {
			p := catalog[l.ProductID]
			if _, err := t.Exec(ctx, `INSERT INTO sale_items (id, sale_id, gym_id, product_id, product_name, quantity, unit_price)
				VALUES ($1,$2,$3,$4,$5,$6,$7::numeric)`, ids.New(), s.ID, s.GymID, l.ProductID, p.name, l.Quantity, p.price); err != nil {
				return err
			}
		}
		rec, err = oneSale(ctx, t, s.GymID, s.ID)
		return err
	})
	switch constraintOf(err) {
	case "sales_pkey", "sales_id_gym_key":
		return app.SaleRecord{}, false, domain.ErrConflict
	}
	return rec, existed, err
}

// Sales lists sales, newest first.
func (r *Repository) Sales(ctx context.Context, gymID uuid.UUID, f app.SaleFilter) ([]app.SaleRecord, int, error) {
	where := ` AND ($2::date IS NULL OR s.sold_on >= $2) AND ($3::date IS NULL OR s.sold_on <= $3)
		AND ($4::uuid[] IS NULL OR s.branch_id = ANY($4)) AND ($5::uuid IS NULL OR s.member_id = $5) AND ($6 = '' OR s.status = $6)`
	var branches any
	if f.BranchIDs != nil {
		branches = f.BranchIDs
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM sales s WHERE s.gym_id = $1`+where, gymID, f.From, f.To, branches, f.MemberID, f.Status).Scan(&total); err != nil {
		return nil, 0, err
	}
	recs, err := loadSales(ctx, r.pool, where, `s.sold_at DESC, s.id`, `LIMIT $7 OFFSET $8`, gymID, f.From, f.To, branches, f.MemberID, f.Status, f.PageSize, (f.Page-1)*f.PageSize)
	if recs == nil {
		recs = []app.SaleRecord{}
	}
	return recs, total, err
}

// PendingBalances lists the members with open pending sales, the biggest balance first.
func (r *Repository) PendingBalances(ctx context.Context, gymID uuid.UUID) ([]app.PendingBalanceRecord, error) {
	rows, err := r.pool.Query(ctx, `SELECT member_id, sum(total)::text, count(*), min(sold_on) FROM sales
		WHERE gym_id = $1 AND status = 'pending' GROUP BY member_id ORDER BY sum(total) DESC, min(sold_on), member_id`, gymID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.PendingBalanceRecord{}
	for rows.Next() {
		var rec app.PendingBalanceRecord
		if err := rows.Scan(&rec.MemberID, &rec.Balance, &rec.PendingCount, &rec.OldestSoldOn); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// MemberPending lists the open pending sales of a member, oldest first.
func (r *Repository) MemberPending(ctx context.Context, gymID, memberID uuid.UUID) ([]app.SaleRecord, error) {
	recs, err := loadSales(ctx, r.pool, `AND s.member_id = $2 AND s.status = 'pending'`, `s.sold_at, s.id`, ``, gymID, memberID)
	if recs == nil {
		recs = []app.SaleRecord{}
	}
	return recs, err
}

// Collect pays whole pending sales of a member in one transaction.
func (r *Repository) Collect(ctx context.Context, s app.NewCollection) (app.CollectionRecord, bool, error) {
	var rec app.CollectionRecord
	existed := false
	remaining := func(t pgx.Tx) (string, error) {
		var left string
		err := t.QueryRow(ctx, `SELECT COALESCE(sum(total), 0)::numeric(14,2)::text FROM sales WHERE gym_id = $1 AND member_id = $2 AND status = 'pending'`,
			s.GymID, s.MemberID).Scan(&left)
		return left, err
	}
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		// A repeated id answers the stored collection.
		var member uuid.UUID
		err := t.QueryRow(ctx, `SELECT id, member_id, branch_id, collected_by, method, amount::text, collected_at FROM sale_collections WHERE id = $1 AND gym_id = $2`,
			s.ID, s.GymID).Scan(&rec.ID, &member, &rec.BranchID, &rec.By, &rec.Method, &rec.Amount, &rec.CollectedAt)
		if err == nil {
			if member != s.MemberID {
				return domain.ErrConflict
			}
			rec.MemberID, existed = member, true
			rows, err := t.Query(ctx, `SELECT id FROM sales WHERE gym_id = $1 AND collection_id = $2 ORDER BY sold_at, id`, s.GymID, s.ID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id uuid.UUID
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				rec.SaleIDs = append(rec.SaleIDs, id)
			}
			rows.Close()
			if rec.Remaining, err = remaining(t); err != nil {
				return err
			}
			return rows.Err()
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Lock the open pending sales to collect. A concurrent collection waits here and then finds them already paid.
		var picked []uuid.UUID
		var cents int64
		wanted := s.SaleIDs
		if wanted == nil {
			wanted = []uuid.UUID{} // a nil slice would be sent as NULL
		}
		rows, err := t.Query(ctx, `SELECT id, total::text FROM sales
			WHERE gym_id = $1 AND member_id = $2 AND status = 'pending' AND (cardinality($3::uuid[]) = 0 OR id = ANY($3))
			ORDER BY id FOR UPDATE`, s.GymID, s.MemberID, wanted)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id uuid.UUID
			var total string
			if err := rows.Scan(&id, &total); err != nil {
				rows.Close()
				return err
			}
			c, _ := domain.ParseMoney(total)
			cents += c
			picked = append(picked, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(s.SaleIDs) == 0 && len(picked) == 0 {
			return domain.ErrNothingToCollect
		}
		if len(s.SaleIDs) > 0 && len(picked) != len(s.SaleIDs) {
			var known int
			if err := t.QueryRow(ctx, `SELECT count(*) FROM sales WHERE gym_id = $1 AND member_id = $2 AND id = ANY($3)`,
				s.GymID, s.MemberID, s.SaleIDs).Scan(&known); err != nil {
				return err
			}
			if known != len(s.SaleIDs) {
				return domain.ErrSaleNotFound
			}
			return domain.ErrSaleNotPending
		}
		if cents > domain.MaxCollectCents {
			return domain.ErrConflict
		}
		rec = app.CollectionRecord{ID: s.ID, MemberID: s.MemberID, BranchID: s.BranchID, By: s.By, Method: s.Method,
			Amount: domain.FormatMoney(cents), SaleIDs: picked}
		if err := t.QueryRow(ctx, `INSERT INTO sale_collections (id, gym_id, member_id, branch_id, method, amount, collected_by, collected_on)
			VALUES ($1,$2,$3,$4,$5,$6::numeric,$7,$8) RETURNING collected_at`,
			s.ID, s.GymID, s.MemberID, s.BranchID, s.Method, rec.Amount, s.By, s.Today).Scan(&rec.CollectedAt); err != nil {
			return err
		}
		if _, err := t.Exec(ctx, `UPDATE sales SET status = 'paid', method = $4, collection_id = $3 WHERE gym_id = $1 AND id = ANY($2)`,
			s.GymID, picked, s.ID, s.Method); err != nil {
			return err
		}
		rec.Remaining, err = remaining(t)
		return err
	})
	switch constraintOf(err) {
	case "sale_collections_pkey", "sale_collections_id_gym_key":
		return app.CollectionRecord{}, false, domain.ErrConflict
	}
	return rec, existed, err
}

// Report sums the sales of the period (sold_on in America/Lima).
func (r *Repository) Report(ctx context.Context, gymID uuid.UUID, f app.ReportFilter) (app.Report, error) {
	out := app.Report{From: f.From.Format("2006-01-02"), To: f.To.Format("2006-01-02"), GroupBy: f.GroupBy, BranchID: f.BranchID,
		ByProduct: []app.ReportProduct{}, ByPeriod: []app.ReportPeriod{}}
	filter := ` s.gym_id = $1 AND s.sold_on BETWEEN $2 AND $3 AND ($4::uuid IS NULL OR s.branch_id = $4)`
	err := r.pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(s.units), 0), COALESCE(sum(s.total), 0)::numeric(14,2)::text,
			COALESCE(sum(s.total) FILTER (WHERE s.status = 'paid'), 0)::numeric(14,2)::text, COALESCE(sum(s.total) FILTER (WHERE s.status = 'pending'), 0)::numeric(14,2)::text
		FROM sales s WHERE`+filter, gymID, f.From, f.To, f.BranchID).
		Scan(&out.Totals.SalesCount, &out.Totals.Units, &out.Totals.TotalSold, &out.Totals.Collected, &out.Totals.Pending)
	if err != nil {
		return out, err
	}
	rows, err := r.pool.Query(ctx, `SELECT i.product_id, COALESCE(p.name, max(i.product_name)), sum(i.quantity)::int, sum(i.quantity * i.unit_price)::text
		FROM sale_items i JOIN sales s ON s.id = i.sale_id AND s.gym_id = i.gym_id
		LEFT JOIN products p ON p.id = i.product_id AND p.gym_id = i.gym_id
		WHERE`+filter+` GROUP BY i.product_id, p.name ORDER BY sum(i.quantity) DESC, COALESCE(p.name, max(i.product_name)), i.product_id`, gymID, f.From, f.To, f.BranchID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v app.ReportProduct
		if err := rows.Scan(&v.ProductID, &v.Name, &v.Units, &v.Total); err != nil {
			rows.Close()
			return out, err
		}
		out.ByProduct = append(out.ByProduct, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	format := "YYYY-MM-DD"
	if f.GroupBy == "month" {
		format = "YYYY-MM"
	}
	periods, err := r.pool.Query(ctx, `SELECT to_char(s.sold_on, $5) AS period, count(*), sum(s.units)::int, sum(s.total)::text
		FROM sales s WHERE`+filter+` GROUP BY period ORDER BY period`, gymID, f.From, f.To, f.BranchID, format)
	if err != nil {
		return out, err
	}
	defer periods.Close()
	for periods.Next() {
		var v app.ReportPeriod
		if err := periods.Scan(&v.Period, &v.SalesCount, &v.Units, &v.Total); err != nil {
			return out, err
		}
		out.ByPeriod = append(out.ByPeriod, v)
	}
	return out, periods.Err()
}
