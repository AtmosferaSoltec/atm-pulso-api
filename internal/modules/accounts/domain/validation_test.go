package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidDocumentNumber(t *testing.T) {
	assert.True(t, ValidDocumentNumber("12345678"))
	for _, bad := range []string{"", "1234567", "123456789", "1234567a", " 12345678"} {
		assert.False(t, ValidDocumentNumber(bad), bad)
	}
}

func TestProfileChanges(t *testing.T) {
	name, phone, err := ProfileChanges("  Ana   María  Pérez ", " 987654321 ")
	require.NoError(t, err)
	assert.Equal(t, "Ana María Pérez", name)
	assert.Equal(t, "987654321", phone)

	_, phone, err = ProfileChanges("Ana Pérez", "")
	require.NoError(t, err)
	assert.Empty(t, phone, "the phone is optional")

	_, _, err = ProfileChanges("A", "12345")
	var validation *ValidationError
	require.ErrorAs(t, err, &validation)
	assert.Len(t, validation.Issues, 2)
}

func TestRoles(t *testing.T) {
	assert.True(t, RoleAdmin.Valid())
	assert.True(t, RoleReceptionist.Valid())
	for _, r := range []Role{"owner", "superadmin", ""} {
		assert.False(t, r.Valid(), string(r))
	}
}
