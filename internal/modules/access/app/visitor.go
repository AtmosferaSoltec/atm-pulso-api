package app

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access/domain"
)

var (
	dniPattern   = regexp.MustCompile(`^[0-9]{8}$`)
	cePattern    = regexp.MustCompile(`^[0-9]{9,12}$`)
	phonePattern = regexp.MustCompile(`^9[0-9]{8}$`)
)

// visitorIssues validates the optional customer data: each field is checked only when present.
func visitorIssues(v Visitor) []domain.Issue {
	var issues []domain.Issue
	if v.DocumentNumber != "" || v.DocumentType != "" {
		ok := (v.DocumentType == "dni" && dniPattern.MatchString(v.DocumentNumber)) ||
			(v.DocumentType == "ce" && cePattern.MatchString(v.DocumentNumber))
		if !ok {
			issues = append(issues, domain.Issue{Field: "visitor.document_number", Code: "DOCUMENT_NUMBER_INVALID"})
		}
	}
	if v.FullName != "" {
		if n := utf8.RuneCountInString(strings.Join(strings.Fields(v.FullName), " ")); n < 3 || n > 120 {
			issues = append(issues, domain.Issue{Field: "visitor.full_name", Code: "FULL_NAME_INVALID"})
		}
	}
	if v.Phone != "" && !phonePattern.MatchString(v.Phone) {
		issues = append(issues, domain.Issue{Field: "visitor.phone", Code: "PHONE_INVALID"})
	}
	return issues
}

// sameMoney compares two decimal strings ("15" equals "15.00").
func sameMoney(a, b string) bool { return normalizeMoney(a) == normalizeMoney(b) }

func normalizeMoney(s string) string {
	whole, frac, _ := strings.Cut(strings.TrimSpace(s), ".")
	frac = (frac + "00")[:2]
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	return whole + "." + frac
}
