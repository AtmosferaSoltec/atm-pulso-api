package app

import (
	"context"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/google/uuid"
)

// UpdateOwner is the platform-only correction/reset path; org inspection never grants writes.
func (s *Service) UpdateOwner(ctx context.Context, p auth.Principal, organizationID uuid.UUID, input UserChanges, reset bool) (TeamUser, error) {
	if err := requirePlatform(p); err != nil {
		return TeamUser{}, err
	}
	if input.Role != nil || input.GymID != nil || input.BranchIDs != nil {
		return TeamUser{}, domain.ErrRoleNotAssignable
	}
	var result TeamUser
	err := s.Team.WithTeam(ctx, organizationID, func(tx TeamTransaction) error {
		users, err := tx.Users(ctx)
		if err != nil {
			return err
		}
		for _, user := range users {
			if user.Credentials.Role != domain.RoleOwner {
				continue
			}
			if input.FullName != nil {
				name, _, err := domain.ProfileChanges(*input.FullName, "")
				if err != nil {
					return err
				}
				user.Credentials.FullName = name
			}
			if input.DocumentNumber != nil && *input.DocumentNumber != user.Credentials.DocumentNumber {
				if !domain.ValidDocumentNumber(*input.DocumentNumber) {
					return fieldError("document_number", "DOCUMENT_NUMBER_INVALID")
				}
				user.Credentials.DocumentNumber = *input.DocumentNumber
				user.Credentials.PasswordVersion++
				if user.Credentials.MustChangePassword {
					reset = true
				}
			}
			if reset {
				hash, err := s.Hasher.Hash(user.Credentials.DocumentNumber)
				if err != nil {
					return err
				}
				user.Credentials.PasswordHash = hash
				user.Credentials.MustChangePassword = true
				user.Credentials.PasswordVersion++
			}
			if err := tx.Save(ctx, user, false); err != nil {
				return err
			}
			result = teamView(user, p.UserID)
			return nil
		}
		return domain.ErrResourceNotFound
	})
	return result, err
}
