// Package app holds the use cases of products: the catalog, quick sales, pending sales and the sales report.
// Persistence and HTTP are ports/adapters.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/peopledir"
)

// BranchInfo is a branch of the gym.
type BranchInfo = branchdir.Info

// Directory reads the gym's branches (owned by accounts; read-only).
type Directory interface {
	Branch(ctx context.Context, gymID, branchID uuid.UUID) (BranchInfo, error)
	Branches(ctx context.Context, gymID uuid.UUID) ([]BranchInfo, error)
}

// People reads members and staff names (owned by other modules; read-only).
type People interface {
	Member(ctx context.Context, gymID, memberID uuid.UUID) (peopledir.Person, error)
	Members(ctx context.Context, gymID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]peopledir.Person, error)
	Users(ctx context.Context, gymID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]peopledir.Person, error)
}

// ProductView is a product of the catalog.
type ProductView struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Price    string    `json:"price"`
	IsActive bool      `json:"is_active"`
	Position int       `json:"position"`
}

// ProductInput is the body of POST /products.
type ProductInput struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Price    string    `json:"price"`
	IsActive *bool     `json:"is_active"`
	// Position: negative or absent puts the product last.
	Position *int `json:"position"`
}

// ProductChanges is the body of PATCH /products/{id}: only the fields sent change.
type ProductChanges struct {
	Name     *string `json:"name"`
	Price    *string `json:"price"`
	IsActive *bool   `json:"is_active"`
	Position *int    `json:"position"`
}

// SaleItemInput is one product of a sale. The unit price always comes from the catalog.
type SaleItemInput struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int       `json:"quantity"`
}

// SaleInput is the body of POST /sales.
type SaleInput struct {
	ID       uuid.UUID `json:"id"`
	BranchID uuid.UUID `json:"branch_id"`
	// MemberID is optional for a paid sale and required for a pending sale.
	MemberID *uuid.UUID `json:"member_id"`
	// Status is "paid" (default) or "pending".
	Status string `json:"status"`
	// Method is required when paid; ignored for a pending sale (the collection carries it).
	Method string          `json:"method"`
	Items  []SaleItemInput `json:"items"`
}

// CollectInput is the body of POST /sales/collections.
type CollectInput struct {
	ID       uuid.UUID `json:"id"`
	MemberID uuid.UUID `json:"member_id"`
	// BranchID is the branch that collects; the caller must be able to work in it.
	BranchID uuid.UUID `json:"branch_id"`
	Method   string    `json:"method"`
	// SaleIDs are the pending sales to collect (whole sales). Absent or empty collects every open pending sale of the member.
	SaleIDs []uuid.UUID `json:"sale_ids"`
}

// SaleItemView is a line of a sale; name and price are a photo of the catalog at the time of the sale.
type SaleItemView struct {
	ProductID uuid.UUID `json:"product_id"`
	Name      string    `json:"name"`
	Quantity  int       `json:"quantity"`
	UnitPrice string    `json:"unit_price"`
	LineTotal string    `json:"line_total"`
}

// MemberRef is the member a sale is charged to.
type MemberRef struct {
	ID             uuid.UUID `json:"id"`
	FullName       string    `json:"full_name"`
	DocumentNumber string    `json:"document_number"`
}

// UserRef is the staff member who did something.
type UserRef struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"full_name"`
}

// BranchRef is a branch by name.
type BranchRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// SaleView is a sale with its items.
type SaleView struct {
	ID     uuid.UUID `json:"id"`
	Branch BranchRef `json:"branch"`
	// Status is "paid" or "pending" (an open pending sale). WasPending says the sale was born as a pending sale, even when collected now.
	Status     string     `json:"status"`
	WasPending bool       `json:"was_pending"`
	Method     *string    `json:"method"`
	Total      string     `json:"total"`
	Units      int        `json:"units"`
	SoldAt     string     `json:"sold_at"`
	SoldOn     string     `json:"sold_on"`
	SoldBy     UserRef    `json:"sold_by"`
	Member     *MemberRef `json:"member"`
	// CollectionID and CollectedAt are set once a pending sale is paid.
	CollectionID *uuid.UUID     `json:"collection_id"`
	CollectedAt  *string        `json:"collected_at"`
	Items        []SaleItemView `json:"items"`
}

// PendingBalance is what a member owes: the sum of their open pending sales.
type PendingBalance struct {
	Member       MemberRef `json:"member"`
	Balance      string    `json:"balance"`
	PendingCount int       `json:"pending_count"`
	OldestSoldOn string    `json:"oldest_sold_on"`
}

// MemberPending is the balance of one member with the sales that make it.
type MemberPending struct {
	MemberID     uuid.UUID  `json:"member_id"`
	Balance      string     `json:"balance"`
	PendingCount int        `json:"pending_count"`
	Sales        []SaleView `json:"sales"`
}

// CollectionView is the result of "Cobrar".
type CollectionView struct {
	ID          uuid.UUID   `json:"id"`
	Member      MemberRef   `json:"member"`
	Branch      BranchRef   `json:"branch"`
	Method      string      `json:"method"`
	Amount      string      `json:"amount"`
	CollectedAt string      `json:"collected_at"`
	CollectedBy UserRef     `json:"collected_by"`
	SaleIDs     []uuid.UUID `json:"sale_ids"`
	// RemainingBalance is what the member still owes after this collection.
	RemainingBalance string `json:"remaining_balance"`
}

// SaleFilter narrows GET /sales. Branches restricts to those branches (nil = every branch of the gym).
type SaleFilter struct {
	From, To  *time.Time
	BranchIDs []uuid.UUID
	MemberID  *uuid.UUID
	Status    string
	Page      int
	PageSize  int
}

// ReportFilter narrows GET /sales/report.
type ReportFilter struct {
	From, To time.Time
	BranchID *uuid.UUID
	// GroupBy is "day" or "month".
	GroupBy string
}

// ReportTotals sum the sales of the period. Pending is the part still owed by members (open pending sales).
type ReportTotals struct {
	SalesCount int    `json:"sales_count"`
	Units      int    `json:"units"`
	TotalSold  string `json:"total_sold"`
	Collected  string `json:"collected_total"`
	Pending    string `json:"pending_total"`
}

// ReportProduct is one product of the period.
type ReportProduct struct {
	ProductID uuid.UUID `json:"product_id"`
	Name      string    `json:"name"`
	Units     int       `json:"units"`
	Total     string    `json:"total"`
}

// ReportPeriod is one day ("2026-10-30") or month ("2026-10").
type ReportPeriod struct {
	Period     string `json:"period"`
	SalesCount int    `json:"sales_count"`
	Units      int    `json:"units"`
	Total      string `json:"total"`
}

// Report is the answer of GET /sales/report.
type Report struct {
	From      string          `json:"from"`
	To        string          `json:"to"`
	GroupBy   string          `json:"group_by"`
	BranchID  *uuid.UUID      `json:"branch_id"`
	Totals    ReportTotals    `json:"totals"`
	ByProduct []ReportProduct `json:"by_product"`
	ByPeriod  []ReportPeriod  `json:"by_period"`
}

// ---- Persistence records (internal) ----

// NewProduct is a product about to be stored.
type NewProduct struct {
	ID       uuid.UUID
	Name     string
	Price    string
	IsActive bool
	// Position: negative puts it last.
	Position int
}

// ProductPatch is a normalized partial update.
type ProductPatch struct {
	Name     *string
	Price    *string
	IsActive *bool
	Position *int
}

// NewSale is a sale about to be stored; the repository resolves the products and the prices in the transaction.
type NewSale struct {
	GymID    uuid.UUID
	ID       uuid.UUID
	BranchID uuid.UUID
	SoldBy   uuid.UUID
	MemberID *uuid.UUID
	Status   string
	Method   string
	Lines    []domain.Line
	Today    time.Time
}

// SaleRecord is a stored sale before names are attached.
type SaleRecord struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	SoldBy       uuid.UUID
	MemberID     *uuid.UUID
	Status       string
	WasPending   bool
	Method       *string
	Total        string
	Units        int
	SoldAt       time.Time
	SoldOn       time.Time
	CollectionID *uuid.UUID
	CollectedAt  *time.Time
	Items        []SaleItemView
}

// NewCollection is a collection about to be stored.
type NewCollection struct {
	GymID    uuid.UUID
	ID       uuid.UUID
	MemberID uuid.UUID
	BranchID uuid.UUID
	By       uuid.UUID
	Method   string
	// SaleIDs empty = every open pending sale of the member.
	SaleIDs []uuid.UUID
	Today   time.Time
}

// CollectionRecord is a stored collection.
type CollectionRecord struct {
	ID          uuid.UUID
	MemberID    uuid.UUID
	BranchID    uuid.UUID
	By          uuid.UUID
	Method      string
	Amount      string
	CollectedAt time.Time
	SaleIDs     []uuid.UUID
	Remaining   string
}

// Repository is the persistence port.
type Repository interface {
	Products(ctx context.Context, gymID uuid.UUID, includeInactive bool) ([]ProductView, error)
	// CreateProduct is idempotent by id: existed is true when the same gym already had a product with that id.
	CreateProduct(ctx context.Context, gymID uuid.UUID, p NewProduct) (view ProductView, existed bool, err error)
	UpdateProduct(ctx context.Context, gymID, id uuid.UUID, patch ProductPatch) (ProductView, error)

	// CreateSale stores a sale with its items in one transaction; existed is true for a repeated id.
	// domain.ValidationError (PRODUCT_INVALID, TOTAL_TOO_LARGE), domain.ErrConflict.
	CreateSale(ctx context.Context, s NewSale) (rec SaleRecord, existed bool, err error)
	Sales(ctx context.Context, gymID uuid.UUID, f SaleFilter) ([]SaleRecord, int, error)
	PendingBalances(ctx context.Context, gymID uuid.UUID) ([]PendingBalanceRecord, error)
	// MemberPending lists the open pending sales of a member, oldest first.
	MemberPending(ctx context.Context, gymID, memberID uuid.UUID) ([]SaleRecord, error)
	// Collect marks whole pending sales as paid in one transaction, locking them. existed is true for a repeated id.
	// domain.ErrSaleNotFound, domain.ErrSaleNotPending, domain.ErrNothingToCollect, domain.ErrConflict.
	Collect(ctx context.Context, s NewCollection) (rec CollectionRecord, existed bool, err error)
	Report(ctx context.Context, gymID uuid.UUID, f ReportFilter) (Report, error)
}

// PendingBalanceRecord is a balance row before the member name is attached.
type PendingBalanceRecord struct {
	MemberID     uuid.UUID
	Balance      string
	PendingCount int
	OldestSoldOn time.Time
}
