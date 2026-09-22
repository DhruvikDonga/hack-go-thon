package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
)

func TestExampleHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewExampleHandler(nil, nil)

	t.Run("CreateItem - Success", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := []byte(`{"name":"Test Item","description":"A test item"}`)
		c.Request = httptest.NewRequest("POST", "/api/v1/items", bytes.NewBuffer(body))
		c.Request.Header.Set("Content-Type", "application/json")

		h.CreateItem(c)

		if w.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d", w.Code)
		}

		var resp response.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if !resp.Success {
			t.Errorf("expected success true")
		}
	})

	t.Run("CreateItem - Validation Error", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := []byte(`{"description":"Missing required name"}`)
		c.Request = httptest.NewRequest("POST", "/api/v1/items", bytes.NewBuffer(body))
		c.Request.Header.Set("Content-Type", "application/json")

		h.CreateItem(c)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("GetItem - Success", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "123"}}
		c.Request = httptest.NewRequest("GET", "/api/v1/items/123", nil)

		h.GetItem(c)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("GetItem - Not Found", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "id", Value: "notfound"}}
		c.Request = httptest.NewRequest("GET", "/api/v1/items/notfound", nil)

		h.GetItem(c)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("ListItems - Success", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/v1/items", nil)

		h.ListItems(c)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("AskAI - Unconfigured LLM", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := []byte(`{"prompt":"Hello"}`)
		c.Request = httptest.NewRequest("POST", "/api/v1/llm/ask", bytes.NewBuffer(body))
		c.Request.Header.Set("Content-Type", "application/json")

		h.AskAI(c)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 when LLM is unconfigured, got %d", w.Code)
		}
	})
}
