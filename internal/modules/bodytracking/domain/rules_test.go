package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/domain"
)

func TestParseTenths(t *testing.T) {
	for raw, want := range map[string]int64{"95": 950, "95.5": 955, " 20 ": 200, "400.0": 4000} {
		got, ok := domain.ParseTenths(raw, domain.WeightKg)
		assert.True(t, ok, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"", "abc", "95.55", "-5", "19.9", "400.1", "95,5", "1e2", ".5", "95.", "NaN"} {
		_, ok := domain.ParseTenths(raw, domain.WeightKg)
		assert.False(t, ok, raw)
	}
	_, ok := domain.ParseTenths("1.9", domain.BodyFatPct)
	assert.False(t, ok)
	_, ok = domain.ParseTenths("70", domain.BodyFatPct)
	assert.True(t, ok)
	assert.Equal(t, "95.5", domain.FormatTenths(955))
	assert.Equal(t, "70.0", domain.FormatTenths(700))
}

func TestCheckDate(t *testing.T) {
	today := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	_, ok := domain.CheckDate(today, today)
	assert.True(t, ok)
	code, ok := domain.CheckDate(today.AddDate(0, 0, 1), today)
	assert.False(t, ok)
	assert.Equal(t, domain.CodeDateFuture, code)
	code, ok = domain.CheckDate(time.Date(1999, 12, 31, 0, 0, 0, 0, time.UTC), today)
	assert.False(t, ok)
	assert.Equal(t, domain.CodeDate, code)
}

func TestTextAndTitle(t *testing.T) {
	text, ok := domain.Text("  a\r\nb\tc  ", 10)
	assert.True(t, ok)
	assert.Equal(t, "a\nb\tc", text)
	_, ok = domain.Text("bad\x00", 10)
	assert.False(t, ok)
	_, ok = domain.Text(strings.Repeat("ñ", 11), 10)
	assert.False(t, ok)
	title, ok := domain.Title("  Fuerza   A ")
	assert.True(t, ok)
	assert.Equal(t, "Fuerza A", title)
	_, ok = domain.Title("   ")
	assert.False(t, ok)
	_, ok = domain.Title(strings.Repeat("x", 81))
	assert.False(t, ok)
}
