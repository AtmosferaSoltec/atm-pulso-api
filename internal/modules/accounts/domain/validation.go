package domain

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	minFullNameLength = 3
	maxFullNameLength = 120
)

var (
	dniPattern   = regexp.MustCompile(`^[0-9]{8}$`)
	phonePattern = regexp.MustCompile(`^9[0-9]{8}$`)
)

// ValidDocumentNumber says whether s is a DNI: exactly 8 digits.
func ValidDocumentNumber(s string) bool { return dniPattern.MatchString(s) }

// NormalizeFullName trims the name and collapses inner whitespace.
func NormalizeFullName(s string) string { return strings.Join(strings.Fields(s), " ") }

// ProfileChanges validates the data a user can edit about themselves.
// It returns the cleaned values, or a *ValidationError.
func ProfileChanges(fullName, phone string) (cleanName, cleanPhone string, err error) {
	var issues []Issue
	cleanName = NormalizeFullName(fullName)
	if n := utf8.RuneCountInString(cleanName); n < minFullNameLength || n > maxFullNameLength {
		issues = append(issues, Issue{Field: "full_name", Code: CodeFullNameInvalid})
	}
	cleanPhone = strings.TrimSpace(phone)
	if cleanPhone != "" && !phonePattern.MatchString(cleanPhone) {
		issues = append(issues, Issue{Field: "phone", Code: CodePhoneInvalid})
	}
	if len(issues) > 0 {
		return "", "", &ValidationError{Issues: issues}
	}
	return cleanName, cleanPhone, nil
}
