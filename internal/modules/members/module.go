// Package members owns members, their membership periods and the receipts of
// memberships. See README.md.
package members

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	membershttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Deps are what the module needs.
type Deps struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
	// Logos resolves gym logos; nil when storage is not configured.
	Logos app.LogoResolver
}

// Module is the assembled members module.
type Module struct{ handler *membershttp.Handler }

// New wires repository, directory, use cases and HTTP by hand.
func New(deps Deps) *Module {
	service := &app.Service{Repo: postgres.NewRepository(deps.Pool), Directory: branchdir.New(deps.Pool), Logos: deps.Logos, Clock: deps.Clock}
	return &Module{handler: membershttp.NewHandler(service)}
}

// Routes mounts the module's endpoints behind authentication.
func (m *Module) Routes(protected gin.IRoutes) {
	m.handler.Routes(protected)
}
