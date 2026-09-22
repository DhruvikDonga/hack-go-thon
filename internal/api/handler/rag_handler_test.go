package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRAGHandler_InMemoryFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewRAGHandler(nil, nil)

	router := gin.New()
	router.POST("/documents", h.CreateDocument)
	router.GET("/documents", h.ListDocuments)
	router.POST("/search", h.SearchDocuments)
	router.POST("/ask", h.AskRAG)

	t.Run("CreateDocument succeeds", func(t *testing.T) {
		payload := map[string]any{
			"title":   "Go Goroutines",
			"content": "Goroutines are lightweight threads managed by the Go runtime.",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d. Body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("CreateDocument validation error on short content", func(t *testing.T) {
		payload := map[string]any{
			"title":   "Short",
			"content": "abc", // min 5
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("ListDocuments returns stored docs", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/documents", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("SearchDocuments returns ranked matches", func(t *testing.T) {
		payload := map[string]any{
			"query": "goroutines",
			"limit": 5,
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/search", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("AskRAG answers question", func(t *testing.T) {
		payload := map[string]any{
			"question": "What is a goroutine?",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/ask", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}
	})
}
