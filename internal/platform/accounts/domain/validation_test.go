package domain

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func issuesOf(t *testing.T, err error) []Issue {
	t.Helper()
	var validation *ValidationError
	require.True(t, errors.As(err, &validation), "expected a *ValidationError, got %v", err)
	return validation.Issues
}

func TestRoleValid(t *testing.T) {
	for _, role := range []Role{RoleSuperadmin, RoleOwner, RoleAdmin, RoleReceptionist} {
		assert.True(t, role.Valid(), role)
	}
	assert.False(t, Role("cashier").Valid())
	assert.False(t, Role("").Valid())
}

func TestValidDocumentNumber(t *testing.T) {
	assert.True(t, ValidDocumentNumber("10000002"))
	for _, bad := range []string{"", "1234567", "123456789", "1234567a", " 12345678", "12345678\n"} {
		assert.False(t, ValidDocumentNumber(bad), "%q", bad)
	}
}

func TestProfileChanges(t *testing.T) {
	t.Run("cleans the values", func(t *testing.T) {
		name, phone, err := ProfileChanges("  Luis   Rojas  ", " 987654321 ")
		require.NoError(t, err)
		assert.Equal(t, "Luis Rojas", name)
		assert.Equal(t, "987654321", phone)
	})
	t.Run("phone is optional", func(t *testing.T) {
		_, phone, err := ProfileChanges("Luis Rojas", "")
		require.NoError(t, err)
		assert.Empty(t, phone)
	})
	t.Run("reports every problem", func(t *testing.T) {
		_, _, err := ProfileChanges("ab", "12345")
		assert.ElementsMatch(t, []Issue{
			{Field: "full_name", Code: CodeFullNameInvalid},
			{Field: "phone", Code: CodePhoneInvalid},
		}, issuesOf(t, err))
	})
	t.Run("name length is counted in characters", func(t *testing.T) {
		_, _, err := ProfileChanges("Ñu", "")
		assert.Error(t, err, "two characters are too short")
		_, _, err = ProfileChanges("Ñuñ", "")
		assert.NoError(t, err)
		_, _, err = ProfileChanges(strings.Repeat("a", maxFullNameLength+1), "")
		assert.Error(t, err)
	})
	t.Run("phone must start with 9", func(t *testing.T) {
		_, _, err := ProfileChanges("Luis Rojas", "887654321")
		assert.Error(t, err)
	})
}

func TestNewPassword(t *testing.T) {
	assert.NoError(t, NewPassword("Pulso2026!", "NuevaClave123"))
	assert.Equal(t, []Issue{{Field: "new_password", Code: CodePasswordTooShort}}, issuesOf(t, NewPassword("Pulso2026!", "corta")))
	assert.Equal(t, []Issue{{Field: "new_password", Code: CodePasswordUnchanged}}, issuesOf(t, NewPassword("Pulso2026!", "Pulso2026!")))
	assert.Equal(t, []Issue{{Field: "new_password", Code: CodePasswordTooLong}}, issuesOf(t, NewPassword("Pulso2026!", strings.Repeat("x", 129))))
}

func TestLoginInput(t *testing.T) {
	assert.NoError(t, LoginInput("10000002", "anything"))
	assert.ElementsMatch(t, []Issue{
		{Field: "document_number", Code: CodeDocumentNumberInvalid},
		{Field: "password", Code: CodePasswordRequired},
	}, issuesOf(t, LoginInput("abc", "")))
}
