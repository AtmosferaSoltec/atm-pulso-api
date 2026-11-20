// Package postgres is the persistence adapter of vouchers. It owns payment_vouchers and sale_vouchers and
// only reads the tables of what a voucher proves: payments (written by the sales modules through
// platform/receipts), and the product sales and collections of the sales module.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

// Repository stores the voucher rows.
type Repository struct{ pool *pgxpool.Pool }

// NewRepository builds the repository.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) PaymentOfReceipt(ctx context.Context, gymID, receiptID uuid.UUID) (app.Owner, error) {
	owner := app.Owner{Kind: domain.OwnerPayment}
	err := r.pool.QueryRow(ctx, `SELECT p.id, p.branch_id, b.gym_id, p.method
		FROM payments p JOIN branches b ON b.id = p.branch_id AND b.gym_id = p.gym_id
		WHERE p.receipt_id = $1 AND p.gym_id = $2`, receiptID, gymID).Scan(&owner.ID, &owner.BranchID, &owner.GymID, &owner.Method)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner, domain.ErrReceiptNotFound
	}
	return owner, err
}

func (r *Repository) Sale(ctx context.Context, gymID, saleID uuid.UUID) (app.SaleInfo, error) {
	info := app.SaleInfo{Owner: app.Owner{Kind: domain.OwnerSale}}
	err := r.pool.QueryRow(ctx, `SELECT id, branch_id, gym_id, COALESCE(method, ''), status, collection_id
		FROM sales WHERE id = $1 AND gym_id = $2`, saleID, gymID).
		Scan(&info.ID, &info.BranchID, &info.GymID, &info.Method, &info.Status, &info.CollectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return info, domain.ErrSaleNotFound
	}
	return info, err
}

func (r *Repository) Collection(ctx context.Context, gymID, collectionID uuid.UUID) (app.Owner, error) {
	owner := app.Owner{Kind: domain.OwnerCollection}
	err := r.pool.QueryRow(ctx, `SELECT id, branch_id, gym_id, method FROM sale_collections WHERE id = $1 AND gym_id = $2`,
		collectionID, gymID).Scan(&owner.ID, &owner.BranchID, &owner.GymID, &owner.Method)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner, domain.ErrCollectionNotFound
	}
	return owner, err
}

// ownerTable says, per kind of owner, the row locked while adding, the voucher table and its owner column.
// The values are constants: never built from input.
type ownerTable struct{ locked, vouchers, column string }

var ownerTables = map[domain.OwnerKind]ownerTable{
	domain.OwnerPayment:    {locked: "payments", vouchers: "payment_vouchers", column: "payment_id"},
	domain.OwnerSale:       {locked: "sales", vouchers: "sale_vouchers", column: "sale_id"},
	domain.OwnerCollection: {locked: "sale_collections", vouchers: "sale_vouchers", column: "collection_id"},
}

func tableOf(kind domain.OwnerKind) (ownerTable, error) {
	table, ok := ownerTables[kind]
	if !ok {
		return ownerTable{}, fmt.Errorf("unknown voucher owner %q", kind)
	}
	return table, nil
}

func notFound(kind domain.OwnerKind) error {
	switch kind {
	case domain.OwnerSale:
		return domain.ErrSaleNotFound
	case domain.OwnerCollection:
		return domain.ErrCollectionNotFound
	default:
		return domain.ErrReceiptNotFound
	}
}

func (r *Repository) Add(ctx context.Context, v app.NewVoucher, max int) error {
	table, err := tableOf(v.Owner.Kind)
	if err != nil {
		return err
	}
	return tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		// Locking the owner's row makes concurrent uploads of one payment, sale or collection take turns.
		var locked uuid.UUID
		if err := t.QueryRow(ctx, `SELECT id FROM `+table.locked+` WHERE id = $1 AND gym_id = $2 FOR UPDATE`, v.Owner.ID, v.GymID).Scan(&locked); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return notFound(v.Owner.Kind)
			}
			return err
		}
		var count int
		if err := t.QueryRow(ctx, `SELECT count(*) FROM `+table.vouchers+` WHERE `+table.column+` = $1 AND gym_id = $2`, v.Owner.ID, v.GymID).Scan(&count); err != nil {
			return err
		}
		if count >= max {
			return domain.ErrLimitReached
		}
		_, err := t.Exec(ctx, `INSERT INTO `+table.vouchers+` (id, gym_id, `+table.column+`, storage_key, content_type, size_bytes, uploaded_by, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, v.ID, v.GymID, v.Owner.ID, v.StorageKey, v.ContentType, v.SizeBytes, v.UploadedBy, v.CreatedAt)
		return err
	})
}

func (r *Repository) List(ctx context.Context, gymID uuid.UUID, owner app.Owner) ([]app.Voucher, error) {
	table, err := tableOf(owner.Kind)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id, content_type, size_bytes, created_at, storage_key, purged_at FROM `+table.vouchers+`
		WHERE gym_id = $1 AND `+table.column+` = $2 ORDER BY created_at, id`, gymID, owner.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.Voucher{}
	for rows.Next() {
		var v app.Voucher
		if err := rows.Scan(&v.ID, &v.ContentType, &v.SizeBytes, &v.CreatedAt, &v.StorageKey, &v.PurgedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Find looks for the voucher among the payments' first and then among the product sales'.
func (r *Repository) Find(ctx context.Context, gymID, voucherID uuid.UUID) (app.Voucher, app.Owner, error) {
	var v app.Voucher
	owner := app.Owner{Kind: domain.OwnerPayment}
	err := r.pool.QueryRow(ctx, `SELECT v.id, v.content_type, v.size_bytes, v.created_at, v.storage_key, v.purged_at, p.id, p.branch_id, b.gym_id, p.method
		FROM payment_vouchers v
		JOIN payments p ON p.id = v.payment_id AND p.gym_id = v.gym_id
		JOIN branches b ON b.id = p.branch_id AND b.gym_id = p.gym_id
		WHERE v.id = $1 AND v.gym_id = $2`, voucherID, gymID).
		Scan(&v.ID, &v.ContentType, &v.SizeBytes, &v.CreatedAt, &v.StorageKey, &v.PurgedAt, &owner.ID, &owner.BranchID, &owner.GymID, &owner.Method)
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return v, owner, err
	}

	var saleID *uuid.UUID
	err = r.pool.QueryRow(ctx, `SELECT v.id, v.content_type, v.size_bytes, v.created_at, v.storage_key, v.purged_at, v.sale_id,
			COALESCE(s.id, c.id), COALESCE(s.branch_id, c.branch_id), v.gym_id, COALESCE(s.method, c.method, '')
		FROM sale_vouchers v
		LEFT JOIN sales s ON s.id = v.sale_id AND s.gym_id = v.gym_id
		LEFT JOIN sale_collections c ON c.id = v.collection_id AND c.gym_id = v.gym_id
		WHERE v.id = $1 AND v.gym_id = $2`, voucherID, gymID).
		Scan(&v.ID, &v.ContentType, &v.SizeBytes, &v.CreatedAt, &v.StorageKey, &v.PurgedAt, &saleID, &owner.ID, &owner.BranchID, &owner.GymID, &owner.Method)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, owner, domain.ErrVoucherNotFound
	}
	owner.Kind = domain.OwnerCollection
	if saleID != nil {
		owner.Kind = domain.OwnerSale
	}
	return v, owner, err
}

func (r *Repository) SaleCounts(ctx context.Context, gymID uuid.UUID, saleIDs, collectionIDs []uuid.UUID) (map[uuid.UUID]int, map[uuid.UUID]int, error) {
	bySale, byCollection := map[uuid.UUID]int{}, map[uuid.UUID]int{}
	if len(saleIDs) == 0 && len(collectionIDs) == 0 {
		return bySale, byCollection, nil
	}
	if saleIDs == nil {
		saleIDs = []uuid.UUID{}
	}
	if collectionIDs == nil {
		collectionIDs = []uuid.UUID{}
	}
	rows, err := r.pool.Query(ctx, `SELECT sale_id, collection_id, count(*) FROM sale_vouchers
		WHERE gym_id = $1 AND (sale_id = ANY($2) OR collection_id = ANY($3))
		GROUP BY sale_id, collection_id`, gymID, saleIDs, collectionIDs)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var saleID, collectionID *uuid.UUID
		var count int
		if err := rows.Scan(&saleID, &collectionID, &count); err != nil {
			return nil, nil, err
		}
		if saleID != nil {
			bySale[*saleID] = count
		} else if collectionID != nil {
			byCollection[*collectionID] = count
		}
	}
	return bySale, byCollection, rows.Err()
}

// Purgeable reads both voucher tables across every gym: the purge is a maintenance job, not a user's request.
// The partial indexes of migration 00026 keep it cheap once most rows are purged.
func (r *Repository) Purgeable(ctx context.Context, before time.Time, limit int) ([]app.Purgeable, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, kind, storage_key FROM (
			(SELECT id, 'payment' AS kind, storage_key, created_at FROM payment_vouchers
				WHERE purged_at IS NULL AND created_at < $1 ORDER BY created_at, id LIMIT $2)
			UNION ALL
			(SELECT id, 'sale' AS kind, storage_key, created_at FROM sale_vouchers
				WHERE purged_at IS NULL AND created_at < $1 ORDER BY created_at, id LIMIT $2)
		) due ORDER BY created_at, id LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.Purgeable{}
	for rows.Next() {
		var v app.Purgeable
		var kind string
		if err := rows.Scan(&v.ID, &kind, &v.StorageKey); err != nil {
			return nil, err
		}
		v.Kind = domain.OwnerKind(kind)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) MarkPurged(ctx context.Context, vouchers []app.Purgeable, at time.Time) error {
	var payments, sales []uuid.UUID
	for _, v := range vouchers {
		if v.Kind == domain.OwnerPayment {
			payments = append(payments, v.ID)
		} else {
			sales = append(sales, v.ID)
		}
	}
	return tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		if len(payments) > 0 {
			if _, err := t.Exec(ctx, `UPDATE payment_vouchers SET purged_at = $1 WHERE id = ANY($2) AND purged_at IS NULL`, at, payments); err != nil {
				return err
			}
		}
		if len(sales) > 0 {
			if _, err := t.Exec(ctx, `UPDATE sale_vouchers SET purged_at = $1 WHERE id = ANY($2) AND purged_at IS NULL`, at, sales); err != nil {
				return err
			}
		}
		return nil
	})
}
