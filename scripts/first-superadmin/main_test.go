package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hash = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5"

func TestStatement(t *testing.T) {
	t.Run("builds the insert for a valid superadmin", func(t *testing.T) {
		sql, err := Statement(" 12345678 ", "  Joel   Maldonado ", hash)
		require.NoError(t, err)
		assert.Contains(t, sql, "SET search_path TO pulso;")
		assert.Contains(t, sql, "INSERT INTO users (id, document_number, full_name, role, password_hash)")
		assert.Contains(t, sql, "gen_random_uuid(), '12345678', 'Joel Maldonado', 'superadmin', '"+hash+"'")
	})
	t.Run("escapes quotes so a name cannot break out of the literal", func(t *testing.T) {
		sql, err := Statement("12345678", "Ana O'Brien", hash)
		require.NoError(t, err)
		assert.Contains(t, sql, "'Ana O''Brien'")
		injection, err := Statement("12345678", "x'); DROP TABLE users; --", hash)
		require.NoError(t, err)
		assert.Contains(t, injection, "''); DROP TABLE users; --'", "the quote is doubled, so it stays inside the string")
		assert.Equal(t, 1, strings.Count(injection, "INSERT INTO"))
	})
	t.Run("refuses bad input", func(t *testing.T) {
		for _, c := range []struct{ doc, name, hash string }{
			{"1234567", "Joel Maldonado", hash},
			{"abc", "Joel Maldonado", hash},
			{"12345678", "ab", hash},
			{"12345678", "Joel Maldonado", "plain-password"},
		} {
			_, err := Statement(c.doc, c.name, c.hash)
			assert.Error(t, err, "%+v", c)
		}
	})
}
