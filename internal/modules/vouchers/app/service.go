// Package app has the use cases of the vouchers module and the ports they need.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
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

// Voucher is one stored proof of payment. The key never leaves the server. ExpiresAt is when its file is
// deleted (nil when files are kept forever) and Expired says the file is gone or due: then it cannot be read.
type Voucher struct {
	ID          uuid.UUID  `json:"id"`
	ContentType string     `json:"content_type"`
	SizeBytes   int        `json:"size_bytes"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Expired     bool       `json:"expired"`
	StorageKey  string     `json:"-"`
	PurgedAt    *time.Time `json:"-"`
}

// Purgeable is a voucher whose file is past the retention period and still in the object store.
type Purgeable struct {
	ID uuid.UUID
	// Kind says the table: domain.OwnerPayment for payment_vouchers, any other for sale_vouchers.
	Kind       domain.OwnerKind
	StorageKey string
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
	// Purgeable lists, oldest first and across every gym, up to limit vouchers created before the instant
	// whose file was not purged yet.
	Purgeable(ctx context.Context, before time.Time, limit int) ([]Purgeable, error)
	// MarkPurged records that the files of those vouchers were deleted. The rows stay.
	MarkPurged(ctx context.Context, vouchers []Purgeable, at time.Time) error
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

// DefaultPurgeBatch is how many vouchers one purge step handles.
const DefaultPurgeBatch = 200

// Service is the vouchers use cases.
type Service struct {
	Repo  Repository
	Store Store // nil when no object store is configured
	// Clock gives "now" for the retention; nil means the system clock.
	Clock clock.Clock
	// Retention is how long files are kept; zero keeps them forever.
	Retention domain.Retention
	// PurgeBatch is how many vouchers one purge step handles; zero means DefaultPurgeBatch.
	PurgeBatch int
}

func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock.Now()
}

// withExpiry fills when the file of each voucher is deleted and whether it already is (or is due).
func (s *Service) withExpiry(vouchers []Voucher) []Voucher {
	now := s.now()
	for i := range vouchers {
		vouchers[i] = s.expiry(vouchers[i], now)
	}
	return vouchers
}

func (s *Service) expiry(v Voucher, now time.Time) Voucher {
	v.ExpiresAt = s.Retention.ExpiresAt(v.CreatedAt)
	v.Expired = s.Retention.Expired(v.CreatedAt, v.PurgedAt != nil, now)
	return v
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
	now := s.now()
	row := NewVoucher{
		Voucher: Voucher{ID: id, ContentType: contentType, SizeBytes: len(data), CreatedAt: now, StorageKey: key},
		GymID:   p.GymID, Owner: owner, UploadedBy: p.UserID,
	}
	if err := s.Repo.Add(ctx, row, domain.MaxPerPayment); err != nil {
		s.discard(key)
		return Voucher{}, err
	}
	return s.expiry(row.Voucher, now), nil
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
	return s.list(ctx, p.GymID, owner)
}

func (s *Service) list(ctx context.Context, gymID uuid.UUID, owner Owner) ([]Voucher, error) {
	vouchers, err := s.Repo.List(ctx, gymID, owner)
	if err != nil {
		return nil, err
	}
	return s.withExpiry(vouchers), nil
}

// ListForSale returns the vouchers of a product sale, oldest first. A pending sale, or one collected later
// (its proofs belong to the collection), has none.
func (s *Service) ListForSale(ctx context.Context, p auth.Principal, saleID uuid.UUID) ([]Voucher, error) {
	sale, err := s.sale(ctx, p, saleID)
	if err != nil {
		return nil, err
	}
	return s.list(ctx, p.GymID, sale.Owner)
}

// ListForCollection returns the vouchers of a collection of pending sales, oldest first.
func (s *Service) ListForCollection(ctx context.Context, p auth.Principal, collectionID uuid.UUID) ([]Voucher, error) {
	owner, err := s.collection(ctx, p, collectionID)
	if err != nil {
		return nil, err
	}
	return s.list(ctx, p.GymID, owner)
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
// A voucher past its retention answers domain.ErrExpired: its file is gone (or about to be).
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
	if v = s.expiry(v, s.now()); v.Expired {
		return Voucher{}, File{}, domain.ErrExpired
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

// Purge deletes from the object store the files of the vouchers past the retention period and marks their rows
// as purged; the rows stay, so counts and history remain. It works in batches until none is left. A file that
// cannot be deleted is logged and skipped (retried on the next run); one already missing counts as deleted.
// Without an object store or without retention it does nothing. It returns how many files it purged.
func (s *Service) Purge(ctx context.Context) (int, error) {
	if s.Store == nil || !s.Retention.Enabled() {
		return 0, nil
	}
	batch := s.PurgeBatch
	if batch <= 0 {
		batch = DefaultPurgeBatch
	}
	now := s.now()
	before := now.Add(-time.Duration(s.Retention))
	purged := 0
	for {
		due, err := s.Repo.Purgeable(ctx, before, batch)
		if err != nil {
			return purged, fmt.Errorf("list purgeable vouchers: %w", err)
		}
		deleted := make([]Purgeable, 0, len(due))
		for _, v := range due {
			if err := s.Store.Delete(ctx, v.StorageKey); err != nil && !errors.Is(err, domain.ErrVoucherNotFound) {
				slog.Warn("could not delete an expired voucher from the object store", "voucher", v.ID, "error", err)
				continue
			}
			deleted = append(deleted, v)
		}
		if len(deleted) > 0 {
			if err := s.Repo.MarkPurged(ctx, deleted, now); err != nil {
				return purged, fmt.Errorf("mark vouchers purged: %w", err)
			}
		}
		purged += len(deleted)
		// A short batch was the last one; a batch where nothing could be deleted would only repeat itself.
		if len(due) < batch || len(deleted) == 0 {
			return purged, nil
		}
	}
}
