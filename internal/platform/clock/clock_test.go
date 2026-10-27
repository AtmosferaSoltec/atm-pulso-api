package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLimaDateFollowsTheLimaCalendar(t *testing.T) {
	// 23:30 in Lima (UTC-5) is already the next day in UTC; the business date is still Lima's.
	late := time.Date(2026, 10, 28, 4, 30, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC), LimaDate(late))
	// 00:30 in Lima is 05:30 UTC: the new Lima day has begun.
	early := time.Date(2026, 10, 28, 5, 30, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), LimaDate(early))
	// Midnight in Lima exactly.
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), LimaDate(time.Date(2026, 1, 1, 5, 0, 0, 0, time.UTC)))
}
