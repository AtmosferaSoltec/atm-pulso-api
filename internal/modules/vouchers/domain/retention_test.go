package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetention(t *testing.T) {
	created := time.Date(2026, 1, 10, 15, 0, 0, 0, time.UTC)
	ninety := RetentionDays(90)
	assert.True(t, ninety.Enabled())
	assert.Equal(t, time.Date(2026, 4, 10, 15, 0, 0, 0, time.UTC), *ninety.ExpiresAt(created))
	assert.False(t, ninety.Expired(created, false, created.Add(89*24*time.Hour)))
	assert.True(t, ninety.Expired(created, false, created.Add(90*24*time.Hour)), "due at the instant it expires")
	assert.True(t, ninety.Expired(created, true, created), "a purged file is gone whatever the date")

	for _, days := range []int{0, -5} {
		forever := RetentionDays(days)
		assert.False(t, forever.Enabled())
		assert.Nil(t, forever.ExpiresAt(created))
		assert.False(t, forever.Expired(created, false, created.AddDate(10, 0, 0)))
		assert.True(t, forever.Expired(created, true, created), "purged while retention was on")
	}
}
