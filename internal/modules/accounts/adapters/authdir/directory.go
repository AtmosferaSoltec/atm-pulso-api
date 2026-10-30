// Package authdir adapts the atm-auth-api client to the accounts module's Directory port,
// turning its failures into domain errors.
package authdir

import (
	"context"
	"errors"
	"net/http"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

// Directory answers from atm-auth-api. Without a client (development without credentials)
// every call reports that the directory is unavailable.
type Directory struct{ client authapi.Client }

// New returns the adapter; client may be nil.
func New(client authapi.Client) *Directory { return &Directory{client: client} }

func (d *Directory) ready() error {
	if d.client == nil {
		return domain.ErrAuthUnavailable
	}
	return nil
}

func translate(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *authapi.Error
	switch {
	case errors.Is(err, authapi.ErrUnavailable):
		return domain.ErrAuthUnavailable
	case errors.Is(err, authapi.ErrNotFound):
		return domain.ErrResourceNotFound
	case errors.As(err, &apiErr):
		switch apiErr.Code {
		case "user_already_in_system":
			return domain.ErrUserInOtherCompany
		case "unknown_role", "invalid_role", "protected_member":
			return domain.ErrRoleNotAssignable
		case "member_not_found":
			return domain.ErrResourceNotFound
		case "invalid_dni":
			return &domain.ValidationError{Issues: []domain.Issue{{Field: "document_number", Code: domain.CodeDocumentNumberInvalid}}}
		case "invalid_full_name":
			return &domain.ValidationError{Issues: []domain.Issue{{Field: "full_name", Code: domain.CodeFullNameInvalid}}}
		case "reset_not_allowed":
			return domain.ErrForbidden
		}
		if apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden {
			// Our own credential or system was refused: nothing the caller can fix.
			return domain.ErrAuthUnavailable
		}
	}
	return err
}

func toTenant(t authapi.Tenant) app.DirectoryTenant {
	return app.DirectoryTenant{ID: t.ID, RUC: t.RUC, Name: t.Name(), ClientID: t.ClientID, ClientName: t.ClientName}
}

func toMember(m authapi.Member) app.DirectoryMember {
	return app.DirectoryMember{UserID: m.UserID, DocumentNumber: m.DNI, FullName: m.FullName, Role: m.Role, Active: m.Status == "" || m.Status == "active"}
}

// Tenants implements app.Directory.
func (d *Directory) Tenants(ctx context.Context) ([]app.DirectoryTenant, error) {
	if err := d.ready(); err != nil {
		return nil, err
	}
	items, err := d.client.Tenants(ctx)
	if err != nil {
		return nil, translate(err)
	}
	out := make([]app.DirectoryTenant, 0, len(items))
	for _, item := range items {
		out = append(out, toTenant(item))
	}
	return out, nil
}

// Tenant implements app.Directory.
func (d *Directory) Tenant(ctx context.Context, tenantID string) (app.DirectoryTenant, error) {
	if err := d.ready(); err != nil {
		return app.DirectoryTenant{}, err
	}
	item, err := d.client.Tenant(ctx, tenantID)
	if err != nil {
		return app.DirectoryTenant{}, translate(err)
	}
	return toTenant(item), nil
}

// Members implements app.Directory.
func (d *Directory) Members(ctx context.Context, tenantID string) ([]app.DirectoryMember, error) {
	if err := d.ready(); err != nil {
		return nil, err
	}
	items, err := d.client.Members(ctx, tenantID)
	if err != nil {
		return nil, translate(err)
	}
	out := make([]app.DirectoryMember, 0, len(items))
	for _, item := range items {
		out = append(out, toMember(item))
	}
	return out, nil
}

// AddMember implements app.Directory.
func (d *Directory) AddMember(ctx context.Context, tenantID string, in app.NewDirectoryMember) (app.DirectoryMember, error) {
	if err := d.ready(); err != nil {
		return app.DirectoryMember{}, err
	}
	item, err := d.client.AddMember(ctx, tenantID, authapi.NewMember{DNI: in.DocumentNumber, FullName: in.FullName, Role: in.Role})
	if err != nil {
		return app.DirectoryMember{}, translate(err)
	}
	return toMember(item), nil
}

// ChangeRole implements app.Directory.
func (d *Directory) ChangeRole(ctx context.Context, tenantID, userID, role string) error {
	if err := d.ready(); err != nil {
		return err
	}
	return translate(d.client.ChangeRole(ctx, tenantID, userID, role))
}

// ResetPassword implements app.Directory.
func (d *Directory) ResetPassword(ctx context.Context, tenantID, userID string) error {
	if err := d.ready(); err != nil {
		return err
	}
	return translate(d.client.ResetPassword(ctx, tenantID, userID))
}
