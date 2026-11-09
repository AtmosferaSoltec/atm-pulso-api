package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

// foreignClaims are the claims atm-auth-api gives a person identified by a document that is not a DNI.
func (h harness) foreignClaims(p person, documentType string) map[string]any {
	claims := h.userClaims(p)
	claims["document_type"], claims["document_number"] = documentType, p.Document
	return claims
}

// People without a Peruvian DNI (a passport, a foreign resident card) work in a gym like anyone else.
func TestPeopleWithForeignDocuments(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	passport := fmt.Sprintf("BO%06d", documentCounter.Add(1)%1_000_000)

	owner := person{
		AuthUserID: uuid.New(), Document: passport, Name: "Dueño Boliviano", Role: "admin",
		ClientID: tn.ClientID, ClientName: tn.GroupName, TenantID: tn.TenantOne,
	}
	ownerToken := h.tokens.Mint(t, h.foreignClaims(owner, "passport"))

	t.Run("a passport opens Pulso and is recorded as such", func(t *testing.T) {
		me := h.call(t, http.MethodGet, "/me", ownerToken, nil)
		require.Equal(t, http.StatusOK, me.status, me.body)
		assert.Equal(t, "passport", me.data()["document_type"])
		assert.Equal(t, passport, me.data()["document_number"])
		assert.Equal(t, "admin", me.data()["role"])
	})

	t.Run("a token whose document does not fit its type is refused", func(t *testing.T) {
		bad := owner
		bad.AuthUserID, bad.Document = uuid.New(), "12345678"
		res := h.call(t, http.MethodGet, "/me", h.tokens.Mint(t, h.foreignClaims(bad, "ce")), nil)
		assert.Equal(t, http.StatusForbidden, res.status, res.body)
	})

	t.Run("the owner adds a receptionist with a foreign resident card", func(t *testing.T) {
		ce := fmt.Sprintf("00%07d", documentCounter.Add(1)%10_000_000)
		created := h.call(t, http.MethodPost, "/users", ownerToken, app.NewUser{
			FullName: "Recepcionista Venezolana", DocumentType: "ce", DocumentNumber: ce,
			Role: domain.RoleReceptionist, BranchIDs: []uuid.UUID{tn.BranchA},
		})
		require.Equal(t, http.StatusCreated, created.status, created.body)
		assert.Equal(t, "ce", created.data()["document_type"])
		assert.Equal(t, ce, created.data()["document_number"])

		// atm-auth-api received the type; the person enters with it.
		members, err := h.auth.Members(context.Background(), tn.TenantOne.String())
		require.NoError(t, err)
		var authUser uuid.UUID
		for _, m := range members {
			if m.DocumentNumber == ce {
				assert.Equal(t, "ce", m.DocumentType)
				authUser = uuid.MustParse(m.UserID)
			}
		}
		require.NotEqual(t, uuid.Nil, authUser, "the person was not created in atm-auth-api")
		receptionist := person{AuthUserID: authUser, Document: ce, Name: "Recepcionista Venezolana", Role: "receptionist",
			ClientID: tn.ClientID, ClientName: tn.GroupName, TenantID: tn.TenantOne}
		me := h.call(t, http.MethodGet, "/me", h.tokens.Mint(t, h.foreignClaims(receptionist, "ce")), nil)
		require.Equal(t, http.StatusOK, me.status, me.body)
		assert.Equal(t, "ce", me.data()["document_type"])

		team := h.call(t, http.MethodGet, "/users", ownerToken, nil)
		require.Equal(t, http.StatusOK, team.status, team.body)
		types := map[string]any{}
		for _, item := range team.body["data"].([]any) {
			user := item.(map[string]any)
			types[user["document_number"].(string)] = user["document_type"]
		}
		assert.Equal(t, "ce", types[ce], "the team lists the type atm-auth-api gives")
	})

	t.Run("a passport is cleaned, and a document that does not fit its type is refused", func(t *testing.T) {
		created := h.call(t, http.MethodPost, "/users", ownerToken, app.NewUser{
			FullName: "Otro Extranjero", DocumentType: "passport", DocumentNumber: " xy-98 7654 ", Role: domain.RoleAdmin,
		})
		require.Equal(t, http.StatusCreated, created.status, created.body)
		assert.Equal(t, "XY987654", created.data()["document_number"])

		for _, input := range []app.NewUser{
			{FullName: "Mal Carné", DocumentType: "ce", DocumentNumber: "12345678", Role: domain.RoleAdmin},
			{FullName: "Mal Pasaporte", DocumentType: "passport", DocumentNumber: "AB1", Role: domain.RoleAdmin},
			{FullName: "Tipo Raro", DocumentType: "rut", DocumentNumber: "123456789", Role: domain.RoleAdmin},
		} {
			res := h.call(t, http.MethodPost, "/users", ownerToken, input)
			assert.Equal(t, http.StatusBadRequest, res.status, input.FullName)
			assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), input.FullName)
		}
	})

	t.Run("the database refuses a document that does not fit its type", func(t *testing.T) {
		_, err := h.pool.Exec(context.Background(),
			`UPDATE users SET document_type = 'dni' WHERE document_number = $1`, passport)
		assert.Error(t, err)
	})
}
