// Package tx runs infrastructure work in a PostgreSQL transaction.
package tx

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Runner owns transaction lifetime; use cases depend on module repository ports.
type Runner struct{ Pool *pgxpool.Pool }

// Run commits only when fn succeeds, otherwise rolling back even if the request was canceled.
func (r Runner) Run(ctx context.Context, fn func(pgx.Tx) error) error {
	transaction, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = transaction.Rollback(cleanup)
	}()
	if err := fn(transaction); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
