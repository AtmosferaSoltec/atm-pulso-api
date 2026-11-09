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

var phonePattern = regexp.MustCompile(`^9[0-9]{8}$`)

// DocumentType is the identity document of a person who works in a gym, as atm-auth-api names it.
type DocumentType string

const (
	// DocumentDNI is the Peruvian national ID: 8 digits.
	DocumentDNI DocumentType = "dni"
	// DocumentCE is the foreign resident card (carné de extranjería): 9 to 12 digits.
	DocumentCE DocumentType = "ce"
	// DocumentPassport is a passport or any other foreign ID: 6 to 12 upper-case letters or digits.
	DocumentPassport DocumentType = "passport"
)

var documentPatterns = map[DocumentType]*regexp.Regexp{
	DocumentDNI:      regexp.MustCompile(`^[0-9]{8}$`),
	DocumentCE:       regexp.MustCompile(`^[0-9]{9,12}$`),
	DocumentPassport: regexp.MustCompile(`^[A-Z0-9]{6,12}$`),
}

// NormalizeDocument cleans a document as atm-auth-api stores it (no spaces or dashes, upper case) and says
// whether the number fits its type. An empty type is a DNI: the only type older tokens and clients know.
func NormalizeDocument(documentType, number string) (DocumentType, string, bool) {
	t := DocumentType(strings.ToLower(strings.TrimSpace(documentType)))
	if t == "" {
		t = DocumentDNI
	}
	number = strings.ToUpper(strings.NewReplacer(" ", "", "-", "", ".", "").Replace(strings.TrimSpace(number)))
	pattern, known := documentPatterns[t]
	if !known || !pattern.MatchString(number) {
		return "", "", false
	}
	return t, number, true
}

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
