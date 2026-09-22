package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPIKeyAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	masterKey := "super-master-api-key"

	setupRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(APIKeyAuth(nil, masterKey))
		r.GET("/secure", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"user_id":      c.GetString("user_id"),
				"api_key_name": c.GetString("api_key_name"),
			})
		})
		return r
	}

	r := setupRouter()

	t.Run("Valid Master API Key", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/secure", nil)
		req.Header.Set("X-API-Key", masterKey)

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Missing API Key Header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/secure", nil)

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("Invalid API Key Header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/secure", nil)
		req.Header.Set("X-API-Key", "unauthorized-key")

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}
