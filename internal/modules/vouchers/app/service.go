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

// Owner is what a voucher proves (a payment, a product sale paid on the spot or a collection of pending
// sales) and where its money was received.
type Owner struct {
	Kind     domain.OwnerKind
	ID       uuid.UUID
	BranchID uuid.UUID
	GymID    uuid.UUID
	Method   string
}

// SaleInfo is what a voucher needs to know of a product sale. Method is empty unless the sale was paid on
// the spot; CollectionID is set once a pending sale was collected.
type SaleInfo struct {
	Owner
	Status       string
	CollectionID *uuid.UUID
}

// Sale statuses, as the sales module stores them.
const (
	saleStatusPaid = "paid"
)

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
	GymID      uuid.UUID
	Owner      Owner
	UploadedBy uuid.UUID
}

// Repository stores the voucher rows. It reads (never writes) the payments, sales and sale_collections
// tables to know what a voucher proves.
type Repository interface {
	// PaymentOfReceipt returns domain.ErrReceiptNotFound when the gym has no such receipt.
	PaymentOfReceipt(ctx context.Context, gymID, receiptID uuid.UUID) (Owner, error)
	// Sale returns domain.ErrSaleNotFound when the gym has no such product sale.
	Sale(ctx context.Context, gymID, saleID uuid.UUID) (SaleInfo, error)
	// Collection returns domain.ErrCollectionNotFound when the gym has no such collection.
	Collection(ctx context.Context, gymID, collectionID uuid.UUID) (Owner, error)
	// Add stores the row unless the owner already has max vouchers (domain.ErrLimitReached).
	// Concurrent uploads of one owner are serialized, so the limit cannot be beaten.
	Add(ctx context.Context, v NewVoucher, max int) error
	List(ctx context.Context, gymID uuid.UUID, owner Owner) ([]Voucher, error)
	// Find returns domain.ErrVoucherNotFound when the gym has no such voucher, whatever it proves.
	Find(ctx context.Context, gymID, voucherID uuid.UUID) (Voucher, Owner, error)
	// SaleCounts counts the vouchers of the given sales and collections of the gym (absent = none).
	SaleCounts(ctx context.Context, gymID uuid.UUID, saleIDs, collectionIDs []uuid.UUID) (bySale, byCollection map[uuid.UUID]int, err error)
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

// reach says whether the user may work with the owner: the same reach as the branch its money was received in.
func reach(p auth.Principal, o Owner) bool {
	return p.CanAccessBranch(auth.BranchRef{ID: o.BranchID, GymID: o.GymID})
}

// Upload stores a voucher for the payment of a receipt. Cash needs none.
func (s *Service) Upload(ctx context.Context, p auth.Principal, receiptID uuid.UUID, data []byte) (Voucher, error) {
	owner, err := s.Repo.PaymentOfReceipt(ctx, p.GymID, receiptID)
	if err != nil {
		return Voucher{}, err
	}
	if !reach(p, owner) {
		return Voucher{}, domain.ErrReceiptNotFound
	}
	return s.store(ctx, p, owner, "receipt", data)
}

// UploadForSale stores a voucher for a product sale paid on the spot. A pending sale has no money to prove
// yet (domain.ErrSaleNotPaid) and a collected one is proven by its collection (domain.ErrSaleCollected).
func (s *Service) UploadForSale(ctx context.Context, p auth.Principal, saleID uuid.UUID, data []byte) (Voucher, error) {
	sale, err := s.sale(ctx, p, saleID)
	if err != nil {
		return Voucher{}, err
	}
	if sale.Status != saleStatusPaid {
		return Voucher{}, domain.ErrSaleNotPaid
	}
	if sale.CollectionID != nil {
		return Voucher{}, domain.ErrSaleCollected
	}
	return s.store(ctx, p, sale.Owner, "sale", data)
}

// UploadForCollection stores a voucher for a collection of pending sales.
func (s *Service) UploadForCollection(ctx context.Context, p auth.Principal, collectionID uuid.UUID, data []byte) (Voucher, error) {
	owner, err := s.collection(ctx, p, collectionID)
	if err != nil {
		return Voucher{}, err
	}
	return s.store(ctx, p, owner, "collection", data)
}

// store checks the file and keeps it for an owner the caller reaches. field names the owner in a cash refusal.
func (s *Service) store(ctx context.Context, p auth.Principal, owner Owner, field string, data []byte) (Voucher, error) {
	if owner.Method == domain.MethodCash {
		return Voucher{}, &domain.ValidationError{Issues: []domain.Issue{{Field: field, Code: domain.CodeNotForCash}}}
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
	key := storageKey(p.GymID, owner, id, extension)
	if err := s.Store.Put(ctx, key, contentType, data); err != nil {
		return Voucher{}, fmt.Errorf("store voucher: %w", err)
	}
	row := NewVoucher{
		Voucher: Voucher{ID: id, ContentType: contentType, SizeBytes: len(data), CreatedAt: time.Now().UTC(), StorageKey: key},
		GymID:   p.GymID, Owner: owner, UploadedBy: p.UserID,
	}
	if err := s.Repo.Add(ctx, row, domain.MaxPerPayment); err != nil {
		s.discard(key)
		return Voucher{}, err
	}
	return row.Voucher, nil
}

// storageKey places the file in the bucket: vouchers/<gym>/<payment>/<id> for memberships (as always),
// vouchers/<gym>/sales/<sale>/<id> and vouchers/<gym>/collections/<collection>/<id> for product sales.
func storageKey(gymID uuid.UUID, owner Owner, id uuid.UUID, extension string) string {
	switch owner.Kind {
	case domain.OwnerSale:
		return fmt.Sprintf("vouchers/%s/sales/%s/%s.%s", gymID, owner.ID, id, extension)
	case domain.OwnerCollection:
		return fmt.Sprintf("vouchers/%s/collections/%s/%s.%s", gymID, owner.ID, id, extension)
	default:
		return fmt.Sprintf("vouchers/%s/%s/%s.%s", gymID, owner.ID, id, extension)
	}
}

// List returns the vouchers of the payment of a receipt, oldest first.
func (s *Service) List(ctx context.Context, p auth.Principal, receiptID uuid.UUID) ([]Voucher, error) {
	owner, err := s.Repo.PaymentOfReceipt(ctx, p.GymID, receiptID)
	if err != nil {
		return nil, err
	}
	if !reach(p, owner) {
		return nil, domain.ErrReceiptNotFound
	}
	return s.Repo.List(ctx, p.GymID, owner)
}

// ListForSale returns the vouchers of a product sale, oldest first. A pending sale, or one collected later
// (its proofs belong to the collection), has none.
func (s *Service) ListForSale(ctx context.Context, p auth.Principal, saleID uuid.UUID) ([]Voucher, error) {
	sale, err := s.sale(ctx, p, saleID)
	if err != nil {
		return nil, err
	}
	return s.Repo.List(ctx, p.GymID, sale.Owner)
}

// ListForCollection returns the vouchers of a collection of pending sales, oldest first.
func (s *Service) ListForCollection(ctx context.Context, p auth.Principal, collectionID uuid.UUID) ([]Voucher, error) {
	owner, err := s.collection(ctx, p, collectionID)
	if err != nil {
		return nil, err
	}
	return s.Repo.List(ctx, p.GymID, owner)
}

func (s *Service) sale(ctx context.Context, p auth.Principal, saleID uuid.UUID) (SaleInfo, error) {
	sale, err := s.Repo.Sale(ctx, p.GymID, saleID)
	if err != nil {
		return SaleInfo{}, err
	}
	if !reach(p, sale.Owner) {
		return SaleInfo{}, domain.ErrSaleNotFound
	}
	return sale, nil
}

func (s *Service) collection(ctx context.Context, p auth.Principal, collectionID uuid.UUID) (Owner, error) {
	owner, err := s.Repo.Collection(ctx, p.GymID, collectionID)
	if err != nil {
		return Owner{}, err
	}
	if !reach(p, owner) {
		return Owner{}, domain.ErrCollectionNotFound
	}
	return owner, nil
}

// Open returns the file of a voucher, whatever it proves. The vouchers of product sales need the sales module.
func (s *Service) Open(ctx context.Context, p auth.Principal, voucherID uuid.UUID) (Voucher, File, error) {
	v, owner, err := s.Repo.Find(ctx, p.GymID, voucherID)
	if err != nil {
		return Voucher{}, File{}, err
	}
	if !reach(p, owner) {
		return Voucher{}, File{}, domain.ErrVoucherNotFound
	}
	if owner.Kind != domain.OwnerPayment && !p.HasFeature(auth.FeatureSales) {
		return Voucher{}, File{}, domain.ErrFeatureDisabled
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

// SaleVoucherCounts counts the vouchers of product sales and collections of a gym. The sales module reads
// it (through its own port, wired in main.go) to show voucher_count on its lists.
func (s *Service) SaleVoucherCounts(ctx context.Context, gymID uuid.UUID, saleIDs, collectionIDs []uuid.UUID) (map[uuid.UUID]int, map[uuid.UUID]int, error) {
	return s.Repo.SaleCounts(ctx, gymID, saleIDs, collectionIDs)
}

// discard deletes a file nobody refers to. A failure only leaves an orphan file, so it is logged.
func (s *Service) discard(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := s.Store.Delete(ctx, key); err != nil {
		slog.Warn("could not delete an unused voucher from the object store", "key", key, "error", err)
	}
}
