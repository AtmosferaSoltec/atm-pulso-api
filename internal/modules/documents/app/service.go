// Package app has the documents module's use cases: look up a DNI or a RUC. The data
// comes from atm-identity-api; Pulso keeps no copy of it.
package app

import (
	"context"
	"errors"
	"regexp"
)

var (
	dniPattern = regexp.MustCompile(`^[0-9]{8}$`)
	rucPattern = regexp.MustCompile(`^[0-9]{11}$`)
)

// Errors of the module.
var (
	ErrInvalidDocument = errors.New("invalid document number")
	ErrNotFound        = errors.New("document not found")
	ErrUnavailable     = errors.New("document lookup unavailable")
)

// Person is a resolved DNI.
type Person struct {
	DocumentNumber  string
	FirstNames      string
	PaternalSurname string
	MaternalSurname string
}

// Company is a resolved RUC.
type Company struct {
	DocumentNumber string
	LegalName      string
	TradeName      string
	Address        string
}

// Directory resolves documents. Implementations translate their own failures into
// ErrNotFound and ErrUnavailable.
type Directory interface {
	Person(ctx context.Context, documentNumber string) (Person, error)
	Company(ctx context.Context, documentNumber string) (Company, error)
}

// Service looks documents up.
type Service struct {
	directory Directory
}

// NewService returns the service.
func NewService(directory Directory) *Service { return &Service{directory: directory} }

// Person resolves a DNI (exactly 8 digits).
func (s *Service) Person(ctx context.Context, documentNumber string) (Person, error) {
	if !dniPattern.MatchString(documentNumber) {
		return Person{}, ErrInvalidDocument
	}
	return s.directory.Person(ctx, documentNumber)
}

// Company resolves a RUC (exactly 11 digits).
func (s *Service) Company(ctx context.Context, documentNumber string) (Company, error) {
	if !rucPattern.MatchString(documentNumber) {
		return Company{}, ErrInvalidDocument
	}
	return s.directory.Company(ctx, documentNumber)
}
