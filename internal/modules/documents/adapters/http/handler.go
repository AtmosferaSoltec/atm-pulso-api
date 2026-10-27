// Package http is the documents module's HTTP adapter.
package http

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/documents/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
)

// Error codes of the documents module.
const (
	CodeDocumentNotFound  = "DOCUMENT_NOT_FOUND"
	CodeLookupUnavailable = "LOOKUP_UNAVAILABLE"
	codeDocumentNumberBad = "DOCUMENT_NUMBER_INVALID"
)

var (
	errNotFound = apierror.New(http.StatusNotFound, CodeDocumentNotFound, "No encontramos este documento. Ingresa los datos a mano.")
	// 502: the failure is upstream, not the user's. The web falls back to manual entry.
	errUnavailable = apierror.New(http.StatusBadGateway, CodeLookupUnavailable, "La búsqueda no está disponible ahora. Ingresa los datos a mano.")
)

type personDTO struct {
	DocumentNumber  string `json:"document_number"`
	FirstNames      string `json:"first_names"`
	PaternalSurname string `json:"paternal_surname"`
	MaternalSurname string `json:"maternal_surname"`
	FullName        string `json:"full_name"`
}

type companyDTO struct {
	DocumentNumber string  `json:"document_number"`
	LegalName      string  `json:"legal_name"`
	TradeName      *string `json:"trade_name"`
	Address        *string `json:"address"`
}

// Handler serves the lookup routes.
type Handler struct {
	service *app.Service
}

// NewHandler returns the handler.
func NewHandler(service *app.Service) *Handler { return &Handler{service: service} }

// Routes mounts the module behind authentication: any signed-in user may look up.
func (h *Handler) Routes(protected gin.IRoutes) {
	protected.GET("/documents/persons/:document_number", h.person)
	protected.GET("/documents/companies/:document_number", h.company)
}

func (h *Handler) person(c *gin.Context) {
	person, err := h.service.Person(c.Request.Context(), c.Param("document_number"))
	if err != nil {
		respond(c, err)
		return
	}
	names := []string{person.FirstNames, person.PaternalSurname, person.MaternalSurname}
	apierror.OK(c, personDTO{
		DocumentNumber:  person.DocumentNumber,
		FirstNames:      person.FirstNames,
		PaternalSurname: person.PaternalSurname,
		MaternalSurname: person.MaternalSurname,
		FullName:        join(names),
	})
}

func (h *Handler) company(c *gin.Context) {
	company, err := h.service.Company(c.Request.Context(), c.Param("document_number"))
	if err != nil {
		respond(c, err)
		return
	}
	apierror.OK(c, companyDTO{
		DocumentNumber: company.DocumentNumber,
		LegalName:      company.LegalName,
		TradeName:      nullable(company.TradeName),
		Address:        nullable(company.Address),
	})
}

func respond(c *gin.Context, err error) {
	switch {
	case errors.Is(err, app.ErrInvalidDocument):
		apierror.Respond(c, apierror.Validation(apierror.FieldError{Field: "document_number", Code: codeDocumentNumberBad}))
	case errors.Is(err, app.ErrNotFound):
		apierror.Respond(c, errNotFound)
	case errors.Is(err, app.ErrUnavailable):
		apierror.Respond(c, errUnavailable)
	default:
		apierror.Respond(c, err)
	}
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func join(parts []string) string {
	out := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += part
	}
	return out
}
