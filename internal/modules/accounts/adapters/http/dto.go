package http

import (
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

type profileRequest struct {
	FullName string `json:"full_name"`
	Phone    string `json:"phone"`
}

type gymDTO struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	BrandColor string  `json:"brand_color"`
	LogoURL    *string `json:"logo_url"`
}

type branchDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	GymID   string `json:"gym_id"`
	GymName string `json:"gym_name"`
}

type userDTO struct {
	ID             string      `json:"id"`
	DocumentNumber string      `json:"document_number"`
	FullName       string      `json:"full_name"`
	Phone          *string     `json:"phone"`
	Role           string      `json:"role"`
	Gym            gymDTO      `json:"gym"`
	Branches       []branchDTO `json:"branches"`
}

func toUserDTO(profile app.Profile, role auth.Role) userDTO {
	user, scope := profile.User, profile.Scope
	dto := userDTO{
		ID:             user.ID.String(),
		DocumentNumber: user.DocumentNumber,
		FullName:       user.FullName,
		Role:           string(role),
		Gym:            gymDTO{ID: scope.Gym.ID.String(), Name: scope.Gym.Name, BrandColor: scope.Gym.BrandColor, LogoURL: profile.LogoURL},
		Branches:       make([]branchDTO, 0, len(scope.Branches)),
	}
	if user.Phone != "" {
		dto.Phone = &user.Phone
	}
	for _, branch := range scope.Branches {
		dto.Branches = append(dto.Branches, branchDTO{
			ID: branch.ID.String(), Name: branch.Name, GymID: branch.GymID.String(), GymName: branch.GymName,
		})
	}
	return dto
}
