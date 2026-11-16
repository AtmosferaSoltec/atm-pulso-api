package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeFeatures(t *testing.T) {
	got, ok := NormalizeFeatures([]string{"body_tracking", "kiosk", "sales", "sales"})
	assert.True(t, ok)
	assert.Equal(t, []Feature{FeatureSales, FeatureBodyTracking}, got, "no repeats, in the fixed order, base modules dropped")

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

func TestBaseFeatures(t *testing.T) {
	assert.True(t, FeatureKiosk.IsBase(), "every gym has the kiosk")
	assert.False(t, FeatureSales.IsBase())
	assert.False(t, FeatureBodyTracking.IsBase())
}
