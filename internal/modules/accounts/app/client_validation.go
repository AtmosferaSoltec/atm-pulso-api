package app

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

var taxPattern = regexp.MustCompile(`^[0-9]{11}$`)
var contactPhonePattern = regexp.MustCompile(`^9[0-9]{8}$`)

func fieldError(field, code string) error {
	return &domain.ValidationError{Issues: []domain.Issue{{Field: field, Code: code}}}
}
func validateClient(input NewClient) error {
	owner := input.Owner
	if !domain.ValidDocumentNumber(owner.DocumentNumber) {
		return fieldError("owner.document_number", "DOCUMENT_NUMBER_INVALID")
	}
	if strings.TrimSpace(owner.FirstNames) == "" || strings.TrimSpace(owner.LastNames) == "" {
		return fieldError("owner.full_name", "FULL_NAME_INVALID")
	}
	if _, _, err := domain.ProfileChanges(owner.FirstNames+" "+owner.LastNames, owner.Phone); err != nil {
		return fieldError("owner.full_name", "FULL_NAME_INVALID")
	}
	if !contactPhonePattern.MatchString(owner.Phone) {
		return fieldError("owner.phone", "PHONE_INVALID")
	}
	if address, err := mail.ParseAddress(owner.Email); err != nil || address.Address != owner.Email {
		return fieldError("owner.email", "EMAIL_INVALID")
	}
	if len(input.Gyms) == 0 {
		return fieldError("gyms", "GYM_REQUIRED")
	}
	for i, gym := range input.Gyms {
		if err := validateGym(gym, fmt.Sprintf("gyms[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}
func validateGym(input NewGym, prefix string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(input.Name)); n < 1 || n > 120 {
		return fieldError(prefix+".name", "NAME_REQUIRED")
	}
	if input.TaxID != "" && !taxPattern.MatchString(input.TaxID) {
		return fieldError(prefix+".tax_id", "TAX_ID_INVALID")
	}
	if len(input.Branches) == 0 {
		return fieldError(prefix+".branches", "BRANCH_REQUIRED")
	}
	for i, branch := range input.Branches {
		if err := validateBranch(branch, fmt.Sprintf("%s.branches[%d]", prefix, i)); err != nil {
			return err
		}
	}
	return nil
}
func validateBranch(input NewBranch, prefix string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(input.Name)); n < 1 || n > 120 {
		return fieldError(prefix+".name", "NAME_REQUIRED")
	}
	return nil
}
