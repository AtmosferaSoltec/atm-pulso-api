// Package sales owns the product catalog, quick sales and pending sales (pending sales). See README.md.
package sales

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	saleshttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/peopledir"
)

// Deps are what the module needs.
type Deps struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
}

// Module is the assembled sales module.
type Module struct{ handler *saleshttp.Handler }

// New wires repository, directories, use cases and HTTP by hand.
func New(deps Deps) *Module {
	service := &app.Service{Repo: postgres.NewRepository(deps.Pool), Directory: branchdir.New(deps.Pool), People: peopledir.New(deps.Pool), Clock: deps.Clock}
	return &Module{handler: saleshttp.NewHandler(service)}
}

// Routes mounts the endpoints behind authentication; each is guarded by the sales module of the gym.
func (m *Module) Routes(protected gin.IRoutes) {
	m.handler.Routes(protected)
}
