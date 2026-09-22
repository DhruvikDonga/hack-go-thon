package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPICallLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(APICallLogger(nil)) // nil db should safely pass through
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ping", nil)

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "pong" {
		t.Errorf("expected pong, got %s", w.Body.String())
	}
}
