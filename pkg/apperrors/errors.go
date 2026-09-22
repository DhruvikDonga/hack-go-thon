package apperrors

import (
	"fmt"
	"net/http"
)

// Common Error Codes
const (
	CodeBadRequest          = "BAD_REQUEST"
	CodeNotFound            = "NOT_FOUND"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeConflict            = "CONFLICT"
	CodeInternalServerError = "INTERNAL_SERVER_ERROR"
	CodeValidationFailed    = "VALIDATION_FAILED"
	CodeTimeout             = "REQUEST_TIMEOUT"
)

// AppError represents a structured, domain-aware application error.
type AppError struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Details    any    `json:"details,omitempty"`
	Err        error  `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

// New creates a custom AppError.
func New(statusCode int, code, message string) *AppError {
	return &AppError{
		StatusCode: statusCode,
		Code:       code,
		Message:    message,
	}
}

// NewBadRequest creates an AppError for 400 Bad Request.
func NewBadRequest(message string, details ...any) *AppError {
	var det any
	if len(details) > 0 {
		det = details[0]
	}
	return &AppError{
		StatusCode: http.StatusBadRequest,
		Code:       CodeBadRequest,
		Message:    message,
		Details:    det,
	}
}

// NewNotFound creates an AppError for 404 Not Found.
func NewNotFound(message string) *AppError {
	return &AppError{
		StatusCode: http.StatusNotFound,
		Code:       CodeNotFound,
		Message:    message,
	}
}

// NewUnauthorized creates an AppError for 401 Unauthorized.
func NewUnauthorized(message string) *AppError {
	return &AppError{
		StatusCode: http.StatusUnauthorized,
		Code:       CodeUnauthorized,
		Message:    message,
	}
}

// NewForbidden creates an AppError for 403 Forbidden.
func NewForbidden(message string) *AppError {
	return &AppError{
		StatusCode: http.StatusForbidden,
		Code:       CodeForbidden,
		Message:    message,
	}
}

// NewConflict creates an AppError for 409 Conflict.
func NewConflict(message string) *AppError {
	return &AppError{
		StatusCode: http.StatusConflict,
		Code:       CodeConflict,
		Message:    message,
	}
}

// NewInternal creates an AppError for 500 Internal Server Error.
func NewInternal(message string, err ...error) *AppError {
	var underlying error
	if len(err) > 0 {
		underlying = err[0]
	}
	return &AppError{
		StatusCode: http.StatusInternalServerError,
		Code:       CodeInternalServerError,
		Message:    message,
		Err:        underlying,
	}
}

// Wrap wraps an existing error into an AppError.
func Wrap(err error, statusCode int, code, message string) *AppError {
	return &AppError{
		StatusCode: statusCode,
		Code:       code,
		Message:    message,
		Err:        err,
	}
}
