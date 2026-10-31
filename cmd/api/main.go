// Single entry point of the API: loads the configuration, assembles the modules and
// starts Gin. The same binary has the subcommands Docker and the production compose use:
//
//	api                     starts the server
//	api migrate [up|down|status]
//	api healthcheck         used by the image's HEALTHCHECK (it ships no curl)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/db"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/gymlogo"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/identity"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/storageapi"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/documents"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/reports"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers"
	vouchersstorage "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/adapters/storage"
	vouchersapp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/app"
	vouchersdomain "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/config"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/database"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/health"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/logger"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/middleware"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/objectstore"
)

const (
	// identityTimeout bounds each call to atm-identity-api.
	identityTimeout = 6 * time.Second
	// authTimeout bounds each call to atm-auth-api.
	authTimeout = 6 * time.Second
	// storageTimeout bounds each call to atm-storage-api.
	storageTimeout = 3 * time.Second
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck())
		case "migrate":
			os.Exit(migrate(os.Args[2:]))
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
			os.Exit(2)
		}
	}
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	log := logger.New(os.Stdout, cfg.IsProduction())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database", "error", err)
		return 1
	}
	defer pool.Close()
	// The database is not required to start: /health/ready says whether it answers,
	// and the pool reconnects by itself when it comes back.
	if err := database.Ping(ctx, pool, 5*time.Second); err != nil {
		log.Warn("database not reachable at startup", "error", err)
	}

	router, api, err := httpserver.NewRouter(httpserver.Options{
		Production:         cfg.IsProduction(),
		Logger:             log,
		CORSOrigins:        cfg.CORSOrigins,
		RateLimitPerMinute: cfg.RateLimitPerMinute,
		BodyLimits: map[string]int64{
			"POST " + httpserver.APIPrefix + "/receipts/:id/vouchers": vouchersdomain.MaxBytes + 1,
		},
	})
	if err != nil {
		log.Error("router", "error", err)
		return 1
	}
	health.Register(api, pool)

	// Without the R2 variables (development) the voucher endpoints answer that storage is unavailable.
	var voucherStore vouchersapp.Store
	if cfg.Storage.Enabled() {
		voucherStore = vouchersstorage.New(objectstore.New(objectstore.Config(cfg.Storage)))
	} else {
		log.Warn("R2 is not configured: payment vouchers cannot be uploaded")
	}

	// Without a service secret (development only) company names fall back to the RUC and the team is unavailable.
	var authClient authapi.Client
	if cfg.Auth.ClientSecret != "" {
		authClient = authapi.NewHTTPClient(cfg.Auth.URL, cfg.Auth.SystemCode, cfg.Auth.ClientSecret, authTimeout)
	} else {
		log.Warn("AUTH_CLIENT_SECRET is not set: company names and team management are unavailable")
	}

	// Gym logos: company profile (atm-auth-api) -> file id -> temporary signed link (atm-storage-api).
	// Without the service secret there is no logo; if either service fails later, logo_url is null.
	var logos interface {
		LogoURL(ctx context.Context, tenantID uuid.UUID) *string
	}
	if httpAuth, ok := authClient.(*authapi.HTTPClient); ok {
		logos = gymlogo.New(authClient, storageapi.NewHTTPClient(cfg.StorageAPI.URL, httpAuth, storageTimeout), log)
	}
	accountsModule := accounts.New(accounts.Deps{Pool: pool, Clock: clock.System{}, Auth: authClient, Logos: logos})

	// Without an API key (development) a fake answers instead of identity-api.
	var identityClient identity.Client = identity.Fake{}
	if cfg.Identity.APIKey != "" {
		identityClient = identity.NewHTTPClient(cfg.Identity.URL, cfg.Identity.APIKey, identityTimeout)
	} else {
		log.Warn("IDENTITY_API_KEY is not set: DNI and RUC lookups answer invented data")
	}
	documentsModule := documents.New(identityClient)

	guard := auth.NewGuard(auth.NewJWKSVerifier(auth.VerifierConfig{JWKSURL: cfg.Auth.JWKSURL(), Issuer: cfg.Auth.Issuer}),
		accountsModule.Resolver(), cfg.Auth.SystemCode)
	protected := api.Group("", guard.User())
	accountsModule.Routes(protected)
	documentsModule.Routes(protected)
	// The platform administrator (console-web) has its own, rate-limited surface.
	heavy := middleware.NewRateLimiter(cfg.LoginRateLimitPerMinute).Middleware()
	accountsModule.AdminRoutes(api.Group("/admin", heavy, guard.Superadmin()))
	members.New(members.Deps{Pool: pool, Clock: clock.System{}, Logos: logos}).Routes(protected)
	access.New(access.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected)
	vouchers.New(vouchers.Deps{Pool: pool, Store: voucherStore}).Routes(protected)
	kiosk.New(kiosk.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected)
	sales.New(sales.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected)
	bodytracking.New(bodytracking.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected)
	// The CSV download is heavy: it shares the tighter per-IP limit.
	reports.New(reports.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected, heavy)

	if err := httpserver.Serve(ctx, router, cfg.Port, log); err != nil {
		log.Error("server", "error", err)
		return 1
	}
	return 0
}

// migrate is invoked by the compose's migrate service before the app starts.
// It only needs DATABASE_URL.
func migrate(args []string) int {
	log := logger.New(os.Stdout, os.Getenv("APP_ENV") == string(config.Production))
	command := database.MigrateUp
	if len(args) > 0 {
		command = args[0]
	}
	databaseURL, err := config.DatabaseURL()
	if err != nil {
		log.Error("migrate", "error", err)
		return 1
	}
	if err := database.Migrate(context.Background(), databaseURL, db.Migrations, command, log); err != nil {
		log.Error("migrate", "error", err)
		return 1
	}
	return 0
}

func healthcheck() int {
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://127.0.0.1:" + port() + httpserver.APIPrefix + "/health")
	if err != nil {
		return 1
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "3001"
}
