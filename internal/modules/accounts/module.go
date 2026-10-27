// Package accounts is the module that owns who can sign in and what they can see:
// organizations, gyms, branches and users, plus login and the user's own profile.
// See README.md.
package accounts

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"

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

// SeedDemo loads the fictional demo accounts. Development only.
func SeedDemo(ctx context.Context, pool *pgxpool.Pool, password string) error {
	hash, err := security.Argon2idHasher{}.Hash(password)
	if err != nil {
		return err
	}
	return postgres.SeedDemo(ctx, pool, hash)
}

// CreateSuperadmin creates a superadmin by hand, since there is no registration. It checks
// the DNI, the name and the password policy and returns *domain.ValidationError for bad
// input and domain.ErrDocumentTaken when the DNI is already a user.
func CreateSuperadmin(ctx context.Context, pool *pgxpool.Pool, documentNumber, fullName, password string) error {
	var issues []domain.Issue
	if !domain.ValidDocumentNumber(documentNumber) {
		issues = append(issues, domain.Issue{Field: "document_number", Code: domain.CodeDocumentNumberInvalid})
	}
	name := domain.NormalizeFullName(fullName)
	if _, _, err := domain.ProfileChanges(name, ""); err != nil {
		issues = append(issues, domain.Issue{Field: "full_name", Code: domain.CodeFullNameInvalid})
	}
	if err := domain.NewPassword("", password); err != nil {
		issues = append(issues, domain.Issue{Field: "password", Code: domain.CodePasswordTooShort})
	}
	if len(issues) > 0 {
		return &domain.ValidationError{Issues: issues}
	}
	hash, err := security.Argon2idHasher{}.Hash(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return postgres.NewRepository(pool).InsertSuperadmin(ctx, ids.New(), documentNumber, name, hash)
}
