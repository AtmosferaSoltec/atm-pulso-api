package app

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

// Client is the platform directory entry.
type Client struct {
	ID    uuid.UUID   `json:"id"`
	Name  string      `json:"name"`
	Owner ClientOwner `json:"owner"`
	Gyms  []Gym       `json:"gyms"`
}

type ClientOwner struct {
	ID             uuid.UUID `json:"id"`
	DocumentNumber string    `json:"document_number"`
	FullName       string    `json:"full_name"`
	IsActive       bool      `json:"is_active"`
}

type ClientSummary struct {
	Clients     int64 `json:"clients"`
	Gyms        int64 `json:"gyms"`
	Branches    int64 `json:"branches"`
	ClientUsers int64 `json:"client_users"`
}

type NewOwner struct {
	DocumentNumber string `json:"document_number"`
	FirstNames     string `json:"first_names"`
	LastNames      string `json:"last_names"`
	Email          string `json:"email"`
	Phone          string `json:"phone"`
}

type NewBranch struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Address string    `json:"address"`
}

type NewGym struct {
	ID       uuid.UUID   `json:"id"`
	Name     string      `json:"name"`
	TaxID    string      `json:"tax_id"`
	Branches []NewBranch `json:"branches"`
}

type NewClient struct {
	ID    uuid.UUID `json:"id"`
	Owner NewOwner  `json:"owner"`
	Gyms  []NewGym  `json:"gyms"`
}

// ClientTransaction runs all writes and plan checks under one organization lock.
type ClientTransaction interface {
	FindClient(context.Context, uuid.UUID) (Client, error)
	InsertClient(context.Context, NewClient, string) error
	AddGym(context.Context, uuid.UUID, NewGym) error
	AddBranch(context.Context, uuid.UUID, uuid.UUID, NewBranch) error
}

type ClientRepository interface {
	WithClient(context.Context, uuid.UUID, func(ClientTransaction) error) error
	ListClients(context.Context, string, int, int) ([]Client, int64, error)
	ClientSummary(context.Context) (ClientSummary, error)
	GymOrganization(context.Context, uuid.UUID) (uuid.UUID, error)
}

func requirePlatform(principal auth.Principal) error {
	if principal.Role != auth.RoleSuperadmin {
		return domain.ErrForbidden
	}
	return nil
}

func (s *Service) ListClients(ctx context.Context, p auth.Principal, search string, page, size int) ([]Client, int64, error) {
	if err := requirePlatform(p); err != nil {
		return nil, 0, err
	}
	clients, total, err := s.Clients.ListClients(ctx, strings.TrimSpace(search), page, size)
	return clients, total, err
}

func (s *Service) ClientSummary(ctx context.Context, p auth.Principal) (ClientSummary, error) {
	if err := requirePlatform(p); err != nil {
		return ClientSummary{}, err
	}
	return s.Clients.ClientSummary(ctx)
}

func (s *Service) CreateClient(ctx context.Context, p auth.Principal, input NewClient) (Client, error) {
	if err := requirePlatform(p); err != nil {
		return Client{}, err
	}
	if err := validateClient(input); err != nil {
		return Client{}, err
	}
	if input.ID == uuid.Nil {
		input.ID = ids.New()
	}
	var result Client
	err := s.Clients.WithClient(ctx, input.ID, func(tx ClientTransaction) error {
		existing, err := tx.FindClient(ctx, input.ID)
		if err == nil {
			result = existing
			return nil
		}
		if err != domain.ErrResourceNotFound {
			return err
		}
		hash, err := s.Hasher.Hash(input.Owner.DocumentNumber)
		if err != nil {
			return err
		}
		if err := tx.InsertClient(ctx, input, hash); err != nil {
			return err
		}
		result, err = tx.FindClient(ctx, input.ID)
		return err
	})
	return result, err
}

func (s *Service) AddGym(ctx context.Context, p auth.Principal, clientID uuid.UUID, input NewGym) (Gym, error) {
	if err := requirePlatform(p); err != nil {
		return Gym{}, err
	}
	if err := validateGym(input, "gym"); err != nil {
		return Gym{}, err
	}
	if input.ID == uuid.Nil {
		input.ID = ids.New()
	}
	var result Gym
	err := s.Clients.WithClient(ctx, clientID, func(tx ClientTransaction) error {
		client, err := tx.FindClient(ctx, clientID)
		if err != nil {
			return err
		}
		for _, gym := range client.Gyms {
			if gym.ID == input.ID {
				result = gym
				return nil
			}
		}
		if err := tx.AddGym(ctx, clientID, input); err != nil {
			return err
		}
		client, err = tx.FindClient(ctx, clientID)
		if err != nil {
			return err
		}
		for _, gym := range client.Gyms {
			if gym.ID == input.ID {
				result = gym
				return nil
			}
		}
		return domain.ErrResourceNotFound
	})
	return result, err
}

func (s *Service) AddBranch(ctx context.Context, p auth.Principal, gymID uuid.UUID, input NewBranch) (Branch, error) {
	if err := requirePlatform(p); err != nil {
		return Branch{}, err
	}
	if err := validateBranch(input, "branch"); err != nil {
		return Branch{}, err
	}
	org, err := s.Clients.GymOrganization(ctx, gymID)
	if err != nil {
		return Branch{}, err
	}
	if input.ID == uuid.Nil {
		input.ID = ids.New()
	}
	var result Branch
	err = s.Clients.WithClient(ctx, org, func(tx ClientTransaction) error {
		client, err := tx.FindClient(ctx, org)
		if err != nil {
			return err
		}
		for _, gym := range client.Gyms {
			if gym.ID != gymID {
				continue
			}
			for _, branch := range gym.Branches {
				if branch.ID == input.ID {
					result = branch
					return nil
				}
			}
			if err := tx.AddBranch(ctx, org, gymID, input); err != nil {
				return err
			}
			result = Branch{ID: input.ID, Name: strings.TrimSpace(input.Name), Accessible: true}
			if input.Address != "" {
				result.Address = &input.Address
			}
			return nil
		}
		return domain.ErrResourceNotFound
	})
	return result, err
}
