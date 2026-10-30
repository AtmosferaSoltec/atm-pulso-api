// Package app has the use cases of the vouchers module and the ports they need.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

// acceptedTypes are the files accepted, by their real content (never by name or declared type).
var acceptedTypes = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "application/pdf": "pdf"}

// PaymentInfo is what a voucher needs to know of the payment it proves.
type PaymentInfo struct {
	ID       uuid.UUID
	BranchID uuid.UUID
	GymID    uuid.UUID
	Method   string
}

// Voucher is one stored proof of payment. The key never leaves the server.
type Voucher struct {
	ID          uuid.UUID `json:"id"`
	ContentType string    `json:"content_type"`
	SizeBytes   int       `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
	StorageKey  string    `json:"-"`
}

// NewVoucher is a voucher about to be stored.
type NewVoucher struct {
	Voucher
	OrganizationID uuid.UUID
	PaymentID      uuid.UUID
	UploadedBy     uuid.UUID
}

// Repository stores the voucher rows.
type Repository interface {
	// PaymentOfReceipt returns domain.ErrReceiptNotFound when the organization has no such receipt.
	PaymentOfReceipt(ctx context.Context, organizationID, receiptID uuid.UUID) (PaymentInfo, error)
	// Add stores the row unless the payment already has max vouchers (domain.ErrLimitReached).
	// Concurrent uploads of one payment are serialized, so the limit cannot be beaten.
	Add(ctx context.Context, v NewVoucher, max int) error
	List(ctx context.Context, organizationID, paymentID uuid.UUID) ([]Voucher, error)
	// Find returns domain.ErrVoucherNotFound when the organization has no such voucher.
	Find(ctx context.Context, organizationID, voucherID uuid.UUID) (Voucher, PaymentInfo, error)
}

// File is a stored voucher being read; the caller closes Body.
type File struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

// Store keeps the files (Cloudflare R2, private bucket).
type Store interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	Delete(ctx context.Context, key string) error
	Get(ctx context.Context, key string) (File, error)
}

// Service is the vouchers use cases.
type Service struct {
	Repo  Repository
	Store Store // nil when no object store is configured
}

// reach says whether the user may work with the payment: the same reach as the branch it was collected in.
func reach(p auth.Principal, info PaymentInfo) bool {
	return p.CanAccessBranch(auth.BranchRef{ID: info.BranchID, GymID: info.GymID})
}

// Upload stores a voucher for the payment of a receipt. Cash needs none.
func (s *Service) Upload(ctx context.Context, p auth.Principal, receiptID uuid.UUID, data []byte) (Voucher, error) {
	info, err := s.Repo.PaymentOfReceipt(ctx, p.OrganizationID, receiptID)
	if err != nil {
		return Voucher{}, err
	}
	if !reach(p, info) {
		return Voucher{}, domain.ErrReceiptNotFound
	}
	if info.Method == "cash" {
		return Voucher{}, &domain.ValidationError{Issues: []domain.Issue{{Field: "receipt", Code: domain.CodeNotForCash}}}
	}
	if s.Store == nil {
		return Voucher{}, domain.ErrStorageUnavailable
	}
	if len(data) == 0 || len(data) > domain.MaxBytes {
		return Voucher{}, &domain.ValidationError{Issues: []domain.Issue{{Field: "voucher", Code: domain.CodeSize}}}
	}
	contentType := http.DetectContentType(data)
	extension, ok := acceptedTypes[contentType]
	if !ok {
		return Voucher{}, &domain.ValidationError{Issues: []domain.Issue{{Field: "voucher", Code: domain.CodeType}}}
	}

	id := ids.New()
	key := fmt.Sprintf("vouchers/%s/%s/%s.%s", p.OrganizationID, info.ID, id, extension)
	if err := s.Store.Put(ctx, key, contentType, data); err != nil {
		return Voucher{}, fmt.Errorf("store voucher: %w", err)
	}
	row := NewVoucher{
		Voucher:        Voucher{ID: id, ContentType: contentType, SizeBytes: len(data), CreatedAt: time.Now().UTC(), StorageKey: key},
		OrganizationID: p.OrganizationID, PaymentID: info.ID, UploadedBy: p.UserID,
	}
	if err := s.Repo.Add(ctx, row, domain.MaxPerPayment); err != nil {
		s.discard(key)
		return Voucher{}, err
	}
	return row.Voucher, nil
}

// List returns the vouchers of the payment of a receipt, oldest first.
func (s *Service) List(ctx context.Context, p auth.Principal, receiptID uuid.UUID) ([]Voucher, error) {
	info, err := s.Repo.PaymentOfReceipt(ctx, p.OrganizationID, receiptID)
	if err != nil {
		return nil, err
	}
	if !reach(p, info) {
		return nil, domain.ErrReceiptNotFound
	}
	return s.Repo.List(ctx, p.OrganizationID, info.ID)
}

// Open returns the file of a voucher.
func (s *Service) Open(ctx context.Context, p auth.Principal, voucherID uuid.UUID) (Voucher, File, error) {
	v, info, err := s.Repo.Find(ctx, p.OrganizationID, voucherID)
	if err != nil {
		return Voucher{}, File{}, err
	}
	if !reach(p, info) {
		return Voucher{}, File{}, domain.ErrVoucherNotFound
	}
	if s.Store == nil {
		return Voucher{}, File{}, domain.ErrStorageUnavailable
	}
	file, err := s.Store.Get(ctx, v.StorageKey)
	if err != nil {
		return Voucher{}, File{}, fmt.Errorf("read voucher: %w", err)
	}
	return v, file, nil
}

// discard deletes a file nobody refers to. A failure only leaves an orphan file, so it is logged.
func (s *Service) discard(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.Store.Delete(ctx, key); err != nil {
		slog.Warn("could not delete an unused voucher from the object store", "key", key, "error", err)
	}
}
