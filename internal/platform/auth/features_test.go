package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeFeatures(t *testing.T) {
	got, ok := NormalizeFeatures([]string{"kiosk", "sales", "kiosk"})
	assert.True(t, ok)
	assert.Equal(t, []Feature{FeatureSales, FeatureKiosk}, got, "no repeats, in the fixed order")

	got, ok = NormalizeFeatures([]string{})
	assert.True(t, ok)
	assert.Empty(t, got)

	for _, bad := range []string{"", "Kiosk", "teleport", " kiosk"} {
		_, ok := NormalizeFeatures([]string{"sales", bad})
		assert.False(t, ok, bad)
	}
}

func TestPrincipalHasFeature(t *testing.T) {
	p := Principal{Features: []Feature{FeatureSales}}
	assert.True(t, p.HasFeature(FeatureSales))
	assert.False(t, p.HasFeature(FeatureKiosk))
	assert.False(t, Principal{}.HasFeature(FeatureSales))
}
