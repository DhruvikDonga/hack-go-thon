package response

import (
	"net/http"

	"hack-go-thon/pkg/apperrors"

	"github.com/gin-gonic/gin"
)

// Response represents the standard JSON API response structure.
type Response struct {
	Success bool          `json:"success"`
	Data    any           `json:"data,omitempty"`
	Error   *ErrorPayload `json:"error,omitempty"`
	Meta    any           `json:"meta,omitempty"`
}

// ErrorPayload represents the error details in the JSON envelope.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// Success sends a success response with arbitrary status code and payload.
func Success(c *gin.Context, statusCode int, data any) {
	c.JSON(statusCode, Response{
		Success: true,
		Data:    data,
	})
}

// OK sends a 200 OK response with the given data.
func OK(c *gin.Context, data any) {
	Success(c, http.StatusOK, data)
}

// Created sends a 201 Created response with the given data.
func Created(c *gin.Context, data any) {
	Success(c, http.StatusCreated, data)
}

// WithMeta sends a 200 OK response with data and metadata (e.g. pagination).
func WithMeta(c *gin.Context, data any, meta any) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Data:    data,
		Meta:    meta,
	})
}

// Error responds with a structured error envelope based on an AppError.
func Error(c *gin.Context, appErr *apperrors.AppError) {
	if appErr == nil {
		appErr = apperrors.NewInternal("An unexpected error occurred")
	}

	c.JSON(appErr.StatusCode, Response{
		Success: false,
		Error: &ErrorPayload{
			Code:    appErr.Code,
			Message: appErr.Message,
			Details: appErr.Details,
		},
	})
}

// AbortWithError sends an error response and immediately stops Gin middleware propagation.
func AbortWithError(c *gin.Context, appErr *apperrors.AppError) {
	Error(c, appErr)
	c.Abort()
}
