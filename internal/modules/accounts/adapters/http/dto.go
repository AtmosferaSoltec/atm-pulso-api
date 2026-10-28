// Package http is the accounts module's HTTP adapter: it translates requests into use
// cases and answers with the API's JSON shape. No business rules live here.
package http

import (
	"time"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
)

type loginRequest struct {
	DocumentNumber string `json:"document_number"`
	Password       string `json:"password"`
}

type profileRequest struct {
	FullName string `json:"full_name"`
	Phone    string `json:"phone"`
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type namedDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type branchDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	GymID   string `json:"gym_id"`
	GymName string `json:"gym_name"`
}

type userDTO struct {
	MustChangePassword bool        `json:"must_change_password"`
	ID                 string      `json:"id"`
	DocumentType       string      `json:"document_type"`
	DocumentNumber     string      `json:"document_number"`
	FullName           string      `json:"full_name"`
	Phone              *string     `json:"phone"`
	Role               string      `json:"role"`
	Organization       *namedDTO   `json:"organization"`
	Gyms               []namedDTO  `json:"gyms"`
	Branches           []branchDTO `json:"branches"`
}

type sessionDTO struct {
	AccessToken string  `json:"access_token"`
	TokenType   string  `json:"token_type"`
	ExpiresAt   string  `json:"expires_at"`
	ExpiresIn   int     `json:"expires_in"`
	User        userDTO `json:"user"`
}

func toUserDTO(profile app.Profile) userDTO {
	user, scope := profile.User, profile.Scope
	dto := userDTO{
		MustChangePassword: user.MustChangePassword,
		ID:                 user.ID.String(),
		DocumentType:       "dni",
		DocumentNumber:     user.DocumentNumber,
		FullName:           user.FullName,
		Role:               string(user.Role),
		Gyms:               make([]namedDTO, 0, len(scope.Gyms)),
		Branches:           make([]branchDTO, 0, len(scope.Branches)),
	}
	if user.Phone != "" {
		dto.Phone = &user.Phone
	}
	if scope.Organization != nil {
		dto.Organization = &namedDTO{ID: scope.Organization.ID.String(), Name: scope.Organization.Name}
	}
	for _, gym := range scope.Gyms {
		dto.Gyms = append(dto.Gyms, namedDTO{ID: gym.ID.String(), Name: gym.Name})
	}
	for _, branch := range scope.Branches {
		dto.Branches = append(dto.Branches, branchDTO{
			ID: branch.ID.String(), Name: branch.Name, GymID: branch.GymID.String(), GymName: branch.GymName,
		})
	}
	return dto
}

func toSessionDTO(session app.Session, now time.Time) sessionDTO {
	return sessionDTO{
		AccessToken: session.Token.Value,
		TokenType:   "Bearer",
		ExpiresAt:   session.Token.ExpiresAt.UTC().Format(time.RFC3339),
		ExpiresIn:   int(session.Token.ExpiresAt.Sub(now).Seconds()),
		User:        toUserDTO(session.Profile),
	}
}
