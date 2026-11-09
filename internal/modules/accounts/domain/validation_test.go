package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDocument(t *testing.T) {
	for _, tc := range []struct {
		documentType, number string
		wantType             DocumentType
		wantNumber           string
	}{
		{"", "12345678", DocumentDNI, "12345678"},
		{"dni", " 12345678 ", DocumentDNI, "12345678"},
		{"ce", "001234567", DocumentCE, "001234567"},
		{"ce", "123456789012", DocumentCE, "123456789012"},
		{"passport", "ab-123 456", DocumentPassport, "AB123456"},
		{"PASSPORT", "123456", DocumentPassport, "123456"},
	} {
		gotType, gotNumber, ok := NormalizeDocument(tc.documentType, tc.number)
		require.True(t, ok, tc.number)
		assert.Equal(t, tc.wantType, gotType, tc.number)
		assert.Equal(t, tc.wantNumber, gotNumber, tc.number)
	}
	for _, bad := range [][2]string{
		{"", ""}, {"dni", "1234567"}, {"dni", "123456789"}, {"dni", "1234567a"}, {"ce", "12345678"},
		{"ce", "1234567890123"}, {"ce", "A12345678"}, {"passport", "12345"}, {"passport", "ABCDEFGHIJ123"},
		{"passport", "AB12345Ñ"}, {"rut", "123456789"},
	} {
		_, _, ok := NormalizeDocument(bad[0], bad[1])
		assert.False(t, ok, "%v", bad)
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
