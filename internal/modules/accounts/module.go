// Package accounts is the module that knows who works where: groups (internal labels that
// bundle gyms), gyms (the companies of atm-auth-api), branches and the people of each gym. Identity itself
// (login, passwords, sessions) belongs to atm-auth-api. See README.md.
package accounts

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/authdir"
	accountshttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Deps are what the module needs to be built.
type Deps struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
	// Auth is the service client of atm-auth-api; nil when Pulso has no credential (development).
	Auth authapi.Client
	// Logos resolves gym logos; nil when storage is not configured.
	Logos app.LogoResolver
}

// Module is the assembled accounts module.
type Module struct {
	repository *postgres.Repository
	service    *app.Service
	handler    *accountshttp.Handler
}

// New wires the module by hand: repository, directory, use cases, HTTP.
func New(deps Deps) *Module {
	repository := postgres.NewRepository(deps.Pool)
	service := app.NewService(app.Deps{
		Tenancy:   repository,
		Users:     repository,
		Gyms:      repository,
		Team:      repository,
		Catalogs:  repository,
		Admin:     repository,
		Directory: authdir.New(deps.Auth),
		Logos:     deps.Logos,
		Clock:     deps.Clock,
	})
	return &Module{repository: repository, service: service, handler: accountshttp.NewHandler(service)}
}

// Resolver turns the verified token of a person into who is asking; it protects every private route.
func (m *Module) Resolver() auth.Resolver { return m.service }

// Routes mounts the routes of people who work in a gym.
func (m *Module) Routes(protected gin.IRoutes) { m.handler.Routes(protected) }

// AdminRoutes mounts the platform administrator's routes.
func (m *Module) AdminRoutes(admin gin.IRoutes) { m.handler.AdminRoutes(admin) }

// Branches exposes tenant-scoped lookup without consumers importing this module.
func (m *Module) Branches() *postgres.Repository { return m.repository }
