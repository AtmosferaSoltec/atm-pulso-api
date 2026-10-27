package domain

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	minFullNameLength = 3
	maxFullNameLength = 120
	// MinPasswordLength matches the web's rule.
	MinPasswordLength = 8
	// maxPasswordLength only protects the hasher from absurd inputs.
	maxPasswordLength = 128
)

var (
	dniPattern   = regexp.MustCompile(`^[0-9]{8}$`)
	phonePattern = regexp.MustCompile(`^9[0-9]{8}$`)
)

// ValidDocumentNumber says whether s is a DNI: exactly 8 digits.
func ValidDocumentNumber(s string) bool { return dniPattern.MatchString(s) }

// NormalizeFullName trims the name and collapses inner whitespace.
func NormalizeFullName(s string) string { return strings.Join(strings.Fields(s), " ") }

// ProfileChanges validates the non-sensitive data a user can edit about themselves.
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

// NewPassword validates a replacement password against the current one.
func NewPassword(current, next string) error {
	var issues []Issue
	switch {
	case utf8.RuneCountInString(next) < MinPasswordLength:
		issues = append(issues, Issue{Field: "new_password", Code: CodePasswordTooShort})
	case len(next) > maxPasswordLength:
		issues = append(issues, Issue{Field: "new_password", Code: CodePasswordTooLong})
	case next == current:
		issues = append(issues, Issue{Field: "new_password", Code: CodePasswordUnchanged})
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}

// LoginInput validates the shape of a login attempt (not whether it is correct).
func LoginInput(documentNumber, password string) error {
	var issues []Issue
	if !ValidDocumentNumber(documentNumber) {
		issues = append(issues, Issue{Field: "document_number", Code: CodeDocumentNumberInvalid})
	}
	if password == "" || len(password) > maxPasswordLength {
		issues = append(issues, Issue{Field: "password", Code: CodePasswordRequired})
	}
	if len(issues) > 0 {
		return &ValidationError{Issues: issues}
	}
	return nil
}
