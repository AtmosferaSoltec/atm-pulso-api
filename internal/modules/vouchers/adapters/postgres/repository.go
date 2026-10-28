// Package postgres is the persistence adapter of vouchers. It reads the payments table
// (written by the sales modules through platform/receipts) to know which payment a receipt
// belongs to and where it was collected.
package postgres

import (
	"context"
	"errors"

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

func (r *Repository) PaymentOfReceipt(ctx context.Context, org, receiptID uuid.UUID) (app.PaymentInfo, error) {
	var info app.PaymentInfo
	err := r.pool.QueryRow(ctx, `SELECT p.id, p.branch_id, b.gym_id, p.method
		FROM payments p JOIN branches b ON b.id = p.branch_id AND b.organization_id = p.organization_id
		WHERE p.receipt_id = $1 AND p.organization_id = $2`, receiptID, org).Scan(&info.ID, &info.BranchID, &info.GymID, &info.Method)
	if errors.Is(err, pgx.ErrNoRows) {
		return info, domain.ErrReceiptNotFound
	}
	return info, err
}

func (r *Repository) Add(ctx context.Context, v app.NewVoucher, max int) error {
	return tx.Runner{Pool: r.pool}.Run(ctx, func(t pgx.Tx) error {
		// Locking the payment row makes concurrent uploads of one payment take turns.
		var locked uuid.UUID
		if err := t.QueryRow(ctx, `SELECT id FROM payments WHERE id = $1 AND organization_id = $2 FOR UPDATE`, v.PaymentID, v.OrganizationID).Scan(&locked); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrReceiptNotFound
			}
			return err
		}
		var count int
		if err := t.QueryRow(ctx, `SELECT count(*) FROM payment_vouchers WHERE payment_id = $1`, v.PaymentID).Scan(&count); err != nil {
			return err
		}
		if count >= max {
			return domain.ErrLimitReached
		}
		_, err := t.Exec(ctx, `INSERT INTO payment_vouchers (id, organization_id, payment_id, storage_key, content_type, size_bytes, uploaded_by, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, v.ID, v.OrganizationID, v.PaymentID, v.StorageKey, v.ContentType, v.SizeBytes, v.UploadedBy, v.CreatedAt)
		return err
	})
}

func (r *Repository) List(ctx context.Context, org, paymentID uuid.UUID) ([]app.Voucher, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, content_type, size_bytes, created_at, storage_key FROM payment_vouchers
		WHERE organization_id = $1 AND payment_id = $2 ORDER BY created_at, id`, org, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.Voucher{}
	for rows.Next() {
		var v app.Voucher
		if err := rows.Scan(&v.ID, &v.ContentType, &v.SizeBytes, &v.CreatedAt, &v.StorageKey); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) Find(ctx context.Context, org, voucherID uuid.UUID) (app.Voucher, app.PaymentInfo, error) {
	var v app.Voucher
	var info app.PaymentInfo
	err := r.pool.QueryRow(ctx, `SELECT v.id, v.content_type, v.size_bytes, v.created_at, v.storage_key, p.id, p.branch_id, b.gym_id, p.method
		FROM payment_vouchers v
		JOIN payments p ON p.id = v.payment_id AND p.organization_id = v.organization_id
		JOIN branches b ON b.id = p.branch_id AND b.organization_id = p.organization_id
		WHERE v.id = $1 AND v.organization_id = $2`, voucherID, org).
		Scan(&v.ID, &v.ContentType, &v.SizeBytes, &v.CreatedAt, &v.StorageKey, &info.ID, &info.BranchID, &info.GymID, &info.Method)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, info, domain.ErrVoucherNotFound
	}
	return v, info, err
}
