package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestJWTAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "super-secret-key-12345"

	setupRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(JWTAuth(secret))
		r.GET("/protected", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"user_id": c.GetString("user_id"),
				"email":   c.GetString("email"),
				"role":    c.GetString("role"),
			})
		})
		return r
	}

	r := setupRouter()

	t.Run("Valid Token", func(t *testing.T) {
		token, err := GenerateToken(secret, "user_1", "user1@example.com", "admin", time.Hour)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Missing Authorization Header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("Malformed Authorization Header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Token some-token")

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("Expired Token", func(t *testing.T) {
		token, err := GenerateToken(secret, "user_1", "user1@example.com", "admin", -time.Hour)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("Invalid Secret Signature", func(t *testing.T) {
		token, err := GenerateToken("wrong-secret-key", "user_1", "user1@example.com", "admin", time.Hour)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}
