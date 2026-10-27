// Package apierror defines the single shape of every API response:
//
//	success:   { "data": ... }
//	paginated: { "data": [...], "meta": { "page", "page_size", "total" } }
//	error:     { "error": { "code", "message", "details" } }
//
// The code is stable and in English: the web picks its own text from it. The
// message is in Spanish, as a fallback the user can read.
package apierror

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/logger"
)

// Codes shared by every module. Each module defines its own.
const (
	CodeValidationFailed    = "VALIDATION_FAILED"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeNotFound            = "NOT_FOUND"
	CodeMethodNotAllowed    = "METHOD_NOT_ALLOWED"
	CodeConflict            = "CONFLICT"
	CodePayloadTooLarge     = "PAYLOAD_TOO_LARGE"
	CodeTooManyRequests     = "TOO_MANY_REQUESTS"
	CodeInternal            = "INTERNAL_ERROR"
	CodeDatabaseUnavailable = "DATABASE_UNAVAILABLE"
)

// Error is an error the API knows how to report to the web.
type Error struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// New creates an error with its HTTP status, code and Spanish message.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// WithDetails returns a copy of the error carrying details (for example the seconds to wait).
func (e *Error) WithDetails(details any) *Error {
	copied := *e
	copied.Details = details
	return &copied
}

// FieldError is a problem with one field; the web shows the first one.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Validation is a 400 listing the problem of each field.
func Validation(fields ...FieldError) *Error {
	return &Error{
		Status:  http.StatusBadRequest,
		Code:    CodeValidationFailed,
		Message: "Revisa los datos enviados.",
		Details: fields,
	}
}

// Ready-made common errors.
var (
	ErrNotFound         = New(http.StatusNotFound, CodeNotFound, "Este registro ya no existe.")
	ErrUnauthorized     = New(http.StatusUnauthorized, CodeUnauthorized, "Tu sesión terminó. Inicia sesión de nuevo.")
	ErrForbidden        = New(http.StatusForbidden, CodeForbidden, "No tienes permiso para hacer esto.")
	ErrMethodNotAllowed = New(http.StatusMethodNotAllowed, CodeMethodNotAllowed, "Esta operación no existe.")
	ErrTooManyRequests  = New(http.StatusTooManyRequests, CodeTooManyRequests, "Demasiados intentos. Espera un momento.")
	ErrPayloadTooLarge  = New(http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "Los datos enviados son demasiado grandes.")
	ErrInternal         = New(http.StatusInternalServerError, CodeInternal, "Algo salió mal. Inténtalo de nuevo.")
	ErrDatabaseDown     = New(http.StatusServiceUnavailable, CodeDatabaseUnavailable, "El servicio no está disponible. Inténtalo en un momento.")
)

type body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// Respond answers with err. An error that is not an *Error is not told to the web:
// it is logged in full and the client only gets INTERNAL_ERROR (a trace may carry data).
func Respond(c *gin.Context, err error) {
	var apiErr *Error
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &apiErr):
	case errors.As(err, &tooLarge):
		apiErr = ErrPayloadTooLarge
	default:
		logger.From(c.Request.Context()).Error("unhandled error", "error", err)
		apiErr = ErrInternal
	}
	c.AbortWithStatusJSON(apiErr.Status, gin.H{"error": body{
		Code:    apiErr.Code,
		Message: apiErr.Message,
		Details: apiErr.Details,
	}})
}

// OK answers 200 with { "data": data }.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// Created answers 201 with { "data": data }.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

// NoContent answers 204 without a body.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Meta is the pagination of a list.
type Meta struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
	// Counts and Totals carry list-specific aggregates (members per status, payment totals).
	Counts any `json:"counts,omitempty"`
	Totals any `json:"totals,omitempty"`
}

// Page answers 200 with { "data": items, "meta": meta }.
func Page(c *gin.Context, items any, meta Meta) {
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": meta})
}
