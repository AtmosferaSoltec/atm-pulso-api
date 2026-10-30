// Package vouchers owns the proofs of payment (photos or PDFs of a transfer, Yape/Plin or card
// receipt) attached to the payment of a receipt. See README.md.
package vouchers

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	vouchershttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/app"
)

// Deps are what the module needs.
type Deps struct {
	Pool *pgxpool.Pool
	// Store keeps the files; nil disables uploads and downloads (development without R2).
	Store app.Store
}

// Module is the assembled vouchers module.
type Module struct{ handler *vouchershttp.Handler }

// New wires repository, use cases and HTTP by hand.
func New(deps Deps) *Module {
	service := &app.Service{Repo: postgres.NewRepository(deps.Pool), Store: deps.Store}
	return &Module{handler: vouchershttp.NewHandler(service)}
}

// Routes mounts the endpoints behind authentication.
func (m *Module) Routes(protected gin.IRoutes) {
	m.handler.Routes(protected)
}
