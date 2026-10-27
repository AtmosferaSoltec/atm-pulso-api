// Package directory adapts the identity-api client to the documents module's port.
package directory

import (
	"context"
	"errors"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/identity"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/documents/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/logger"
)

// Identity resolves documents through atm-identity-api.
type Identity struct {
	client identity.Client
}

// New wraps client.
func New(client identity.Client) *Identity { return &Identity{client: client} }

// Person resolves a DNI.
func (d *Identity) Person(ctx context.Context, documentNumber string) (app.Person, error) {
	person, err := d.client.Person(ctx, documentNumber)
	if err != nil {
		return app.Person{}, translate(ctx, err)
	}
	return app.Person(person), nil
}

// Company resolves a RUC.
func (d *Identity) Company(ctx context.Context, documentNumber string) (app.Company, error) {
	company, err := d.client.Company(ctx, documentNumber)
	if err != nil {
		return app.Company{}, translate(ctx, err)
	}
	return app.Company(company), nil
}

func translate(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, identity.ErrNotFound):
		return app.ErrNotFound
	case errors.Is(err, identity.ErrRejected):
		// Our key was refused: the operator must fix the configuration. The document
		// number is never logged, it is personal data.
		logger.From(ctx).Error("identity api rejected our api key")
		return app.ErrUnavailable
	default:
		logger.From(ctx).Warn("identity api unavailable", "error", err)
		return app.ErrUnavailable
	}
}
