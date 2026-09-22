package apperrors

import (
	"errors"
	"net/http"
	"testing"
)

func TestAppErrors(t *testing.T) {
	t.Run("NewBadRequest", func(t *testing.T) {
		err := NewBadRequest("missing parameter", map[string]string{"field": "email"})
		if err.StatusCode != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, err.StatusCode)
		}
		if err.Code != CodeBadRequest {
			t.Errorf("expected code %s, got %s", CodeBadRequest, err.Code)
		}
		if err.Message != "missing parameter" {
			t.Errorf("expected message 'missing parameter', got %s", err.Message)
		}
	})

	t.Run("NewNotFound", func(t *testing.T) {
		err := NewNotFound("user not found")
		if err.StatusCode != http.StatusNotFound {
			t.Errorf("expected status %d, got %d", http.StatusNotFound, err.StatusCode)
		}
		if err.Code != CodeNotFound {
			t.Errorf("expected code %s, got %s", CodeNotFound, err.Code)
		}
	})

	t.Run("NewInternal and Unwrap", func(t *testing.T) {
		rootErr := errors.New("db connection failure")
		err := NewInternal("internal error", rootErr)
		if err.StatusCode != http.StatusInternalServerError {
			t.Errorf("expected status %d, got %d", http.StatusInternalServerError, err.StatusCode)
		}
		if !errors.Is(err, rootErr) {
			t.Errorf("expected errors.Is to match root error")
		}
	})

	t.Run("Wrap", func(t *testing.T) {
		rootErr := errors.New("query failed")
		err := Wrap(rootErr, http.StatusConflict, CodeConflict, "conflict detected")
		if err.StatusCode != http.StatusConflict {
			t.Errorf("expected status %d, got %d", http.StatusConflict, err.StatusCode)
		}
		if err.Unwrap() != rootErr {
			t.Errorf("expected unwrap to return rootErr")
		}
	})
}
