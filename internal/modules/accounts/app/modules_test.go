package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModulesCatalog(t *testing.T) {
	byCode := map[string]Module{}
	for _, m := range Modules() {
		byCode[m.Code] = m
		assert.NotNil(t, m.Requires, m.Code)
	}
	assert.Equal(t, ModuleBase, byCode["members"].Kind)
	assert.False(t, byCode["members"].Switchable)
	assert.Equal(t, ModuleBase, byCode["kiosk"].Kind, "the kiosk is for every gym")
	assert.False(t, byCode["kiosk"].Switchable)
	assert.Equal(t, ModuleOptional, byCode["sales"].Kind)
	assert.True(t, byCode["sales"].Switchable)
	assert.True(t, byCode["body_tracking"].Switchable)
}

func TestFeaturesOfAlwaysHasBase(t *testing.T) {
	assert.Equal(t, []string{"kiosk"}, KnownFeatures(nil))
	assert.Equal(t, []string{"sales", "kiosk"}, KnownFeatures([]string{"kiosk", "sales", "gone"}))
}
