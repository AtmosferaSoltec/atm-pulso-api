// Package reports serves the read-only payments report, its CSV download and the
// dashboard. It owns no tables. See README.md.
package reports

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	reportshttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/reports/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/reports/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/reports/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Deps are what the module needs.
type Deps struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
}

// Module is the assembled reports module.
type Module struct{ handler *reportshttp.Handler }

// New wires repository, directory, use cases and HTTP by hand.
func New(deps Deps) *Module {
	service := &app.Service{Repo: postgres.NewRepository(deps.Pool), Directory: branchdir.New(deps.Pool), Clock: deps.Clock}
	return &Module{handler: reportshttp.NewHandler(service)}
}

// Routes mounts the endpoints; exportLimit is the stricter per-IP limiter of the heavy download.
func (m *Module) Routes(protected gin.IRoutes, exportLimit gin.HandlerFunc) {
	m.handler.Routes(protected, exportLimit)
}
