package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
)

type mockChecker struct {
	name string
	err  error
}

func (m *mockChecker) Name() string {
	return m.name
}

func (m *mockChecker) Check(ctx context.Context) error {
	return m.err
}

func TestHealthHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Live Probe", func(t *testing.T) {
		h := NewHealthHandler()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		h.Live(c)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Ready Probe - Healthy", func(t *testing.T) {
		h := NewHealthHandler()
		h.RegisterChecker(&mockChecker{name: "db", err: nil})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/health/ready", nil)

		h.Ready(c)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp response.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.Success {
			t.Errorf("expected success to be true")
		}
	})

	t.Run("Ready Probe - Degraded", func(t *testing.T) {
		h := NewHealthHandler()
		h.RegisterChecker(&mockChecker{name: "db", err: errors.New("db unreachable")})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/health/ready", nil)

		h.Ready(c)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", w.Code)
		}

		var resp response.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Success {
			t.Errorf("expected success to be false when dependency is down")
		}
	})
}
