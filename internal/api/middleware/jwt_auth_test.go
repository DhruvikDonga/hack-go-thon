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

	t.Run("User Token With Metadata and RequireAuthLevel", func(t *testing.T) {
		r2 := gin.New()
		r2.Use(JWTAuth(secret))
		r2.GET("/admin-only", RequireAuthLevel(50), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"username":   c.GetString("username"),
				"phone":      c.GetString("phone_number"),
				"auth_level": c.MustGet("auth_level"),
			})
		})

		// Low level user
		lowToken, err := GenerateTokenWithMetadata(secret, "u1", "alice", "alice@example.com", "12345", "member", 10, map[string]any{"dept": "ops"}, time.Hour)
		if err != nil {
			t.Fatalf("failed to generate low level token: %v", err)
		}

		wLow := httptest.NewRecorder()
		reqLow := httptest.NewRequest("GET", "/admin-only", nil)
		reqLow.Header.Set("Authorization", "Bearer "+lowToken)
		r2.ServeHTTP(wLow, reqLow)
		if wLow.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for auth level 10 on route requiring 50, got %d", wLow.Code)
		}

		// High level user
		highToken, err := GenerateTokenWithMetadata(secret, "u2", "admin", "admin@example.com", "99999", "admin", 99, map[string]any{"dept": "core"}, time.Hour)
		if err != nil {
			t.Fatalf("failed to generate high level token: %v", err)
		}

		wHigh := httptest.NewRecorder()
		reqHigh := httptest.NewRequest("GET", "/admin-only", nil)
		reqHigh.Header.Set("Authorization", "Bearer "+highToken)
		r2.ServeHTTP(wHigh, reqHigh)
		if wHigh.Code != http.StatusOK {
			t.Errorf("expected 200 OK for auth level 99, got %d", wHigh.Code)
		}
	})

	t.Run("RequireRole Verification", func(t *testing.T) {
		r3 := gin.New()
		r3.Use(JWTAuth(secret))
		r3.GET("/moderators", RequireRole("moderator", "admin"), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		userToken, _ := GenerateToken(secret, "u1", "user@test.com", "user", time.Hour)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/moderators", nil)
		req.Header.Set("Authorization", "Bearer "+userToken)
		r3.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403 for role user, got %d", w.Code)
		}

		adminToken, _ := GenerateToken(secret, "u2", "admin@test.com", "admin", time.Hour)
		wAdmin := httptest.NewRecorder()
		reqAdmin := httptest.NewRequest("GET", "/moderators", nil)
		reqAdmin.Header.Set("Authorization", "Bearer "+adminToken)
		r3.ServeHTTP(wAdmin, reqAdmin)
		if wAdmin.Code != http.StatusOK {
			t.Errorf("expected 200 for role admin, got %d", wAdmin.Code)
		}
	})

	t.Run("Query Parameter Token and RequireAuthLevelRange", func(t *testing.T) {
		r4 := gin.New()
		r4.Use(JWTAuth(secret))
		r4.GET("/ws", RequireAuthLevelRange(10, 99), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ws-authorized"})
		})

		// 1. Level 1 user token (should be rejected from 10-99 range)
		lvl1Token, _ := GenerateTokenWithMetadata(secret, "u_low", "demo", "demo@test.com", "", "member", 1, nil, time.Hour)
		w1 := httptest.NewRecorder()
		req1 := httptest.NewRequest("GET", "/ws?token="+lvl1Token, nil)
		r4.ServeHTTP(w1, req1)
		if w1.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden for level 1 on range 10-99, got %d", w1.Code)
		}

		// 2. Level 10 user token (should be accepted)
		lvl10Token, _ := GenerateTokenWithMetadata(secret, "u_op", "operator", "op@test.com", "", "support", 10, nil, time.Hour)
		w10 := httptest.NewRecorder()
		req10 := httptest.NewRequest("GET", "/ws?token="+lvl10Token, nil)
		r4.ServeHTTP(w10, req10)
		if w10.Code != http.StatusOK {
			t.Errorf("expected 200 OK for level 10 on range 10-99, got %d", w10.Code)
		}

		// 3. Level 99 admin token (should be accepted)
		lvl99Token, _ := GenerateTokenWithMetadata(secret, "u_adm", "admin", "admin@hack-go-thon.local", "9427425572", "admin", 99, nil, time.Hour)
		w99 := httptest.NewRecorder()
		req99 := httptest.NewRequest("GET", "/ws?token="+lvl99Token, nil)
		r4.ServeHTTP(w99, req99)
		if w99.Code != http.StatusOK {
			t.Errorf("expected 200 OK for level 99 on range 10-99, got %d", w99.Code)
		}

		// 4. No token (should be 401 Unauthorized)
		wNone := httptest.NewRecorder()
		reqNone := httptest.NewRequest("GET", "/ws", nil)
		r4.ServeHTTP(wNone, reqNone)
		if wNone.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for no token, got %d", wNone.Code)
		}
	})
}
