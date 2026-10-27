// Package accounts is the module that owns who can sign in and what they can see:
// organizations, gyms, branches and users, plus login and the user's own profile.
// See README.md.
package accounts

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	accountshttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/security"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Deps are what the module needs to be built.
type Deps struct {
	Pool           *pgxpool.Pool
	Clock          clock.Clock
	JWTSecret      string
	AccessTokenTTL time.Duration
}

// Module is the assembled accounts module.
type Module struct {
	service *app.Service
	handler *accountshttp.Handler
}

// New wires the module by hand: repository, crypto adapters, use cases, HTTP.
func New(deps Deps) (*Module, error) {
	jwt := security.NewJWT(deps.JWTSecret, deps.AccessTokenTTL)
	service, err := app.NewService(app.Deps{
		Users:    postgres.NewRepository(deps.Pool),
		Hasher:   security.Argon2idHasher{},
		Issuer:   jwt,
		Verifier: jwt,
		Clock:    deps.Clock,
	})
	if err != nil {
		return nil, err
	}
	return &Module{service: service, handler: accountshttp.NewHandler(service, deps.Clock)}, nil
}

// Authenticator is what protects every private route of every module.
func (m *Module) Authenticator() auth.Authenticator { return m.service }

// Routes mounts the module's endpoints.
func (m *Module) Routes(limited, protected gin.IRoutes) { m.handler.Routes(limited, protected) }
