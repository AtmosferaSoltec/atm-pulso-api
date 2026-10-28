// Package logos adapts the object store to the accounts module's LogoStore port.
package logos

import (
	"context"
	"errors"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/objectstore"
)

// Store keeps the logo files in an object store.
type Store struct{ client *objectstore.Client }

// New wraps the client.
func New(client *objectstore.Client) Store { return Store{client: client} }

func (s Store) Put(ctx context.Context, key, contentType string, data []byte) error {
	return s.client.Put(ctx, key, contentType, data)
}

func (s Store) Delete(ctx context.Context, key string) error { return s.client.Delete(ctx, key) }

func (s Store) Get(ctx context.Context, key string) (app.LogoFile, error) {
	object, err := s.client.Get(ctx, key)
	if errors.Is(err, objectstore.ErrNotFound) {
		return app.LogoFile{}, domain.ErrResourceNotFound
	}
	if err != nil {
		return app.LogoFile{}, err
	}
	return app.LogoFile{Body: object.Body, ContentType: object.ContentType, Size: object.Size}, nil
}
