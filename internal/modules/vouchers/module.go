// Package vouchers owns the proofs of payment (photos or PDFs of a transfer, Yape/Plin or card
// receipt) attached to the payment of a receipt, to a product sale paid on the spot or to a collection
// of pending sales. See README.md.
package vouchers

import (
	"context"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	vouchershttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

const (
	// purgeDelay is how long after start the first purge runs (the server is up and warm by then).
	purgeDelay = 2 * time.Minute
	// purgeEvery is how often the purge runs afterwards.
	purgeEvery = 6 * time.Hour
)

// Deps are what the module needs.
type Deps struct {
	Pool *pgxpool.Pool
	// Store keeps the files; nil disables uploads and downloads (development without R2).
	Store app.Store
	// Clock gives "now" for the retention; nil means the system clock.
	Clock clock.Clock
	// RetentionDays is how long files are kept (VOUCHER_RETENTION_DAYS); zero or less keeps them forever.
	RetentionDays int
}

// Module is the assembled vouchers module.
type Module struct {
	service *app.Service
	handler *vouchershttp.Handler
}

// New wires repository, use cases and HTTP by hand.
func New(deps Deps) *Module {
	service := &app.Service{Repo: postgres.NewRepository(deps.Pool), Store: deps.Store, Clock: deps.Clock,
		Retention: domain.RetentionDays(deps.RetentionDays)}
	return &Module{service: service, handler: vouchershttp.NewHandler(service)}
}

// BodyLimits gives every upload route (under prefix, the API prefix) room for one voucher, for
// httpserver.Options.BodyLimits.
func BodyLimits(prefix string) map[string]int64 {
	limits := make(map[string]int64, len(vouchershttp.UploadRoutes))
	for _, path := range vouchershttp.UploadRoutes {
		limits["POST "+prefix+path] = domain.MaxBytes + 1
	}
	return limits
}

// SaleVoucherCounts counts the vouchers of product sales and collections; main.go hands it to the sales
// module, which shows voucher_count on its lists without reading this module's tables.
func (m *Module) SaleVoucherCounts(ctx context.Context, gymID uuid.UUID, saleIDs, collectionIDs []uuid.UUID) (map[uuid.UUID]int, map[uuid.UUID]int, error) {
	return m.service.SaleVoucherCounts(ctx, gymID, saleIDs, collectionIDs)
}

// Routes mounts the endpoints behind authentication.
func (m *Module) Routes(protected gin.IRoutes) {
	m.handler.Routes(protected)
}

// PurgeExpired deletes the files past the retention period once (see app.Service.Purge) and says how many.
func (m *Module) PurgeExpired(ctx context.Context) (int, error) {
	return m.service.Purge(ctx)
}

// RunPurge deletes expired files shortly after start and then every six hours, until ctx ends. It returns at
// once when there is nothing to do: no object store or no retention. main.go runs it in its own goroutine.
func (m *Module) RunPurge(ctx context.Context) {
	if m.service.Store == nil || !m.service.Retention.Enabled() {
		slog.Info("voucher purge disabled: no object store or retention is off")
		return
	}
	timer := time.NewTimer(purgeDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		purged, err := m.service.Purge(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("voucher purge", "error", err, "purged", purged)
		} else if purged > 0 {
			slog.Info("voucher purge", "purged", purged)
		}
		timer.Reset(purgeEvery)
	}
}
