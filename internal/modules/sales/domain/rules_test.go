package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestMoney(t *testing.T) {
	for raw, want := range map[string]int64{"10": 1000, "10.5": 1050, "10.50": 1050, "0.01": 1, "0": 0, "1234567.89": 123456789} {
		got, ok := ParseMoney(raw)
		assert.True(t, ok, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{"", "-1", "1.234", "1,5", "abc", "12345678", "1e3", " "} {
		_, ok := ParseMoney(raw)
		assert.False(t, ok, raw)
	}
	assert.Equal(t, "10.00", FormatMoney(1000))
	assert.Equal(t, "0.05", FormatMoney(5))
	assert.Equal(t, "1234.56", FormatMoney(123456))
}

func TestProductPriceAndName(t *testing.T) {
	_, ok := ProductPrice("0")
	assert.False(t, ok, "free products are not sold")
	_, ok = ProductPrice("10000.01")
	assert.False(t, ok)
	cents, ok := ProductPrice("10000.00")
	assert.True(t, ok)
	assert.Equal(t, int64(MaxPriceCents), cents)
	name, ok := ProductName("  Batido   de  fresa ")
	assert.True(t, ok)
	assert.Equal(t, "Batido de fresa", name)
	_, ok = ProductName("   ")
	assert.False(t, ok)
	_, ok = ProductName(string(make([]rune, 61)))
	assert.False(t, ok)
}

func TestMergeLines(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	got, ok := MergeLines([]Line{{a, 1}, {b, 2}, {a, 3}})
	assert.True(t, ok)
	assert.Equal(t, []Line{{a, 4}, {b, 2}}, got)
	for name, lines := range map[string][]Line{
		"none": nil, "zero": {{a, 0}}, "negative": {{a, -1}}, "too many units": {{a, 100}},
		"merged over the limit": {{a, 60}, {a, 60}}, "nil product": {{uuid.Nil, 1}},
	} {
		_, ok := MergeLines(lines)
		assert.False(t, ok, name)
	}
	many := make([]Line, 0, MaxItems+1)
	for i := 0; i <= MaxItems; i++ {
		many = append(many, Line{uuid.New(), 1})
	}
	_, ok = MergeLines(many)
	assert.False(t, ok, "too many different products")
}
