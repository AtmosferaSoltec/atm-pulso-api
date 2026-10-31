// Package kiosk owns the settings of the access tablet (kiosk mode): the exit PIN. See README.md.
package kiosk

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk/adapters/crypto"
	kioskhttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Deps are what the module needs.
type Deps struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
}

// Module is the assembled kiosk module.
type Module struct{ handler *kioskhttp.Handler }

// New wires repository, hasher, use cases and HTTP by hand.
func New(deps Deps) *Module {
	service := &app.Service{Repo: postgres.NewRepository(deps.Pool), Hasher: crypto.Bcrypt{}, Clock: deps.Clock}
	return &Module{handler: kioskhttp.NewHandler(service)}
}

// Routes mounts the endpoints behind authentication.
func (m *Module) Routes(protected gin.IRoutes) {
	m.handler.Routes(protected)
}
