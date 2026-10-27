// Package clock gives the current time behind an interface, so tests can fix "now"
// without depending on the system clock.
package clock

import "time"

// Clock returns the current instant.
type Clock interface {
	Now() time.Time
}

// System is the real clock, always in UTC.
type System struct{}

// Now returns the system time in UTC.
func (System) Now() time.Time { return time.Now().UTC() }

// Fixed is a clock stopped at one instant, for tests.
type Fixed struct {
	At time.Time
}

// Now always returns the same instant.
func (f Fixed) Now() time.Time { return f.At }

// lima is UTC-5 all year (Peru has no daylight saving time), so a fixed zone is exact
// and does not depend on the tz database being installed in the image.
var lima = time.FixedZone("America/Lima", -5*60*60)

// LimaDate returns the calendar date in Lima of an instant, as midnight UTC. Every
// business date (member status, receipts, payments) is a Lima date.
func LimaDate(t time.Time) time.Time {
	local := t.In(lima)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}
