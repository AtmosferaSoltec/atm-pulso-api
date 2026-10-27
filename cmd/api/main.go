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

	"github.com/AtmosferaSoltec/atm-pulso-api/db"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/identity"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/documents"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/config"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/database"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/health"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/logger"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/middleware"
)

// identityTimeout bounds each call to atm-identity-api.
const identityTimeout = 6 * time.Second

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
	})
	if err != nil {
		log.Error("router", "error", err)
		return 1
	}
	health.Register(api, pool)

	accountsModule, err := accounts.New(accounts.Deps{
		Pool:           pool,
		Clock:          clock.System{},
		JWTSecret:      cfg.JWTAccessSecret,
		AccessTokenTTL: cfg.AccessTokenTTL,
	})
	if err != nil {
		log.Error("modules", "error", err)
		return 1
	}

	// Without an API key (development) a fake answers instead of identity-api.
	var identityClient identity.Client = identity.Fake{}
	if cfg.Identity.APIKey != "" {
		identityClient = identity.NewHTTPClient(cfg.Identity.URL, cfg.Identity.APIKey, identityTimeout)
	} else {
		log.Warn("IDENTITY_API_KEY is not set: DNI and RUC lookups answer invented data")
	}
	documentsModule := documents.New(identityClient)

	// Login has a tighter per-IP limit than the rest of the API.
	limited := api.Group("", middleware.NewRateLimiter(cfg.LoginRateLimitPerMinute).Middleware())
	protected := api.Group("", auth.Require(accountsModule.Authenticator()))
	accountsModule.Routes(limited, protected)
	documentsModule.Routes(protected)
	scope := auth.OrganizationScope(accountsModule.Organizations())
	members.New(members.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected, scope)

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
