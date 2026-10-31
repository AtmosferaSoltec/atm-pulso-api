// Package domain holds the pure rules of the body_tracking module: dated body measurements and routines of a member.
// These are SENSITIVE personal data (Peruvian law 29733). Decimals have one digit and travel as tenths. No I/O.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits, repeated as CHECK constraints in the migration (value ranges are inclusive, in tenths of the unit).
const (
	MaxNotesLength   = 500
	MaxTitleLength   = 80
	MaxContentLength = 5000
)

// EarliestDate is the oldest date a record may carry.
var EarliestDate = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// Range is an inclusive range in tenths.
type Range struct{ Min, Max int64 }

// Ranges of every measurement, in tenths (weight 20.0 to 400.0 kg, ...).
var (
	WeightKg   = Range{200, 4000}
	HeightCm   = Range{500, 2500}
	BodyFatPct = Range{20, 700}
	GirthCm    = Range{200, 3000} // waist, chest and hip
)

// Validation codes (stable; the web picks the text).
const (
	CodeDate         = "DATE_INVALID"
	CodeDateFuture   = "DATE_FUTURE"
	CodeWeight       = "WEIGHT_INVALID"
	CodeHeight       = "HEIGHT_INVALID"
	CodeBodyFat      = "BODY_FAT_INVALID"
	CodeWaist        = "WAIST_INVALID"
	CodeChest        = "CHEST_INVALID"
	CodeHip          = "HIP_INVALID"
	CodeValuesNeeded = "VALUE_REQUIRED"
	CodeNotes        = "NOTES_INVALID"
	CodeTitle        = "TITLE_INVALID"
	CodeContent      = "CONTENT_INVALID"
	CodeID           = "ID_INVALID"
)

// Errors answered by the module.
var (
	ErrMemberNotFound      = errors.New("member not found")
	ErrMeasurementNotFound = errors.New("measurement not found")
	ErrRoutineNotFound     = errors.New("routine not found")
	ErrBranchNotAccessible = errors.New("branch not accessible")
	ErrForbidden           = errors.New("forbidden")
	// ErrConflict: the client id already belongs to a record of another gym or member (or was deleted).
	ErrConflict = errors.New("conflict")
)

// Issue is one problem found in a field.
type Issue struct{ Field, Code string }

// ValidationError groups the problems of one request.
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string { return "validation failed" }

var decimalPattern = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9])?$`)

// ParseTenths reads "95", "95.5" into tenths (955) and checks the range. Two decimals or text are invalid.
func ParseTenths(raw string, r Range) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if !decimalPattern.MatchString(raw) {
		return 0, false
	}
	whole, frac, _ := strings.Cut(raw, ".")
	var tenths int64
	for _, digit := range whole {
		tenths = tenths*10 + int64(digit-'0')
	}
	tenths *= 10
	if frac != "" {
		tenths += int64(frac[0] - '0')
	}
	return tenths, tenths >= r.Min && tenths <= r.Max
}

// FormatTenths writes tenths as the decimal text of the API, "95.5".
func FormatTenths(tenths int64) string { return fmt.Sprintf("%d.%d", tenths/10, tenths%10) }

// CheckDate validates a record date against today in Lima: not in the future, not before the year 2000.
func CheckDate(date, today time.Time) (string, bool) {
	switch {
	case date.Before(EarliestDate):
		return CodeDate, false
	case date.After(today):
		return CodeDateFuture, false
	}
	return "", true
}

// Text cleans free text: line breaks become \n, edges are trimmed and control characters (except \n and tab) are
// rejected. The length is counted in characters.
func Text(raw string, max int) (string, bool) {
	text := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n"))
	for _, r := range text {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return "", false
		}
	}
	return text, utf8.RuneCountInString(text) <= max
}

// Title collapses the spaces of a one-line title.
func Title(raw string) (string, bool) {
	title := strings.Join(strings.Fields(raw), " ")
	n := utf8.RuneCountInString(title)
	return title, n >= 1 && n <= MaxTitleLength
}
