// Package access owns the access checks, their history and the day passes. See README.md.
package access

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	accesshttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Deps are what the module needs.
type Deps struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
}

// Module is the assembled access module.
type Module struct{ handler *accesshttp.Handler }

// New wires repository, directory, use cases and HTTP by hand.
func New(deps Deps) *Module {
	service := &app.Service{Repo: postgres.NewRepository(deps.Pool), Directory: branchdir.New(deps.Pool), Clock: deps.Clock}
	return &Module{handler: accesshttp.NewHandler(service)}
}

// Routes mounts the module's endpoints behind authentication.
func (m *Module) Routes(protected gin.IRoutes) {
	m.handler.Routes(protected)
}
