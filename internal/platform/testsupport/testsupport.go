// Package testsupport has helpers for the integration tests.
package testsupport

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/db"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/database"
)

// DatabaseURLVar points to a local PostgreSQL. The deploy check has no database:
// without this variable the integration tests are skipped.
const DatabaseURLVar = "TEST_DATABASE_URL"

// DatabaseURL returns the test database URL, or skips the test.
func DatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv(DatabaseURLVar)
	if url == "" {
		t.Skipf("%s is not set: integration test skipped", DatabaseURLVar)
	}
	return url
}

// Pool opens a pool against the test database and closes it when the test ends.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := database.Open(context.Background(), DatabaseURL(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

var (
	migrateOnce sync.Once
	migrateErr  error
)

// MigratedPool applies the pending migrations (once per test package) and opens a
// pool. The tests of every module start here.
func MigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := DatabaseURL(t)
	migrateOnce.Do(func() {
		quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
		migrateErr = database.Migrate(context.Background(), url, db.Migrations, database.MigrateUp, quiet)
	})
	require.NoError(t, migrateErr)
	return Pool(t)
}
