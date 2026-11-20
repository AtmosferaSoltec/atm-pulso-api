// Package bodytracking owns dated body measurements and routines of members (sensitive data) and their access log.
// See README.md.
package bodytracking

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	bodyhttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/export"
)

// Deps are what the module needs.
type Deps struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
	// Brands gives the letterhead of the PDF; nil draws Pulso's.
	Brands export.BrandSource
}

// Module is the assembled body_tracking module.
type Module struct {
	handler *bodyhttp.Handler
	exports *bodyhttp.ExportHandler
}

// New wires repository, use cases and HTTP by hand.
func New(deps Deps) *Module {
	repo := postgres.NewRepository(deps.Pool)
	service := &app.Service{Repo: repo, Cards: repo, Clock: deps.Clock}
	return &Module{handler: bodyhttp.NewHandler(service), exports: bodyhttp.NewExportHandler(service, deps.Brands)}
}

// Routes mounts the endpoints behind authentication; each is guarded by the body_tracking module of the gym.
func (m *Module) Routes(protected gin.IRoutes) {
	m.handler.Routes(protected)
}

// ExportRoutes mounts the measurements PDF; limit is the stricter per-IP limiter of heavy requests.
func (m *Module) ExportRoutes(protected gin.IRoutes, limit gin.HandlerFunc) {
	m.exports.Routes(protected, limit)
}
