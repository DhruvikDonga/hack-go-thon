package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hack-go-thon/config"
	"hack-go-thon/internal/api/handler"
	"hack-go-thon/internal/jobs"
	"hack-go-thon/internal/ws"
)

func TestRouter_AdminAndRAG(t *testing.T) {
	cfg := &config.Config{
		Environment:    "test",
		AllowedOrigins: []string{"*"},
		JWTSecret:      "test-secret",
	}

	scheduler := jobs.NewScheduler()
	scheduler.RegisterFunc("sample_cron", 30*time.Second, func(ctx context.Context) error {
		return nil
	})

	healthH := handler.NewHealthHandler()
	exampleH := handler.NewExampleHandler(nil, nil)
	ragH := handler.NewRAGHandler(nil, nil)
	adminH := ws.NewAdminRoomHandler(nil, scheduler)
	wsMgr := ws.NewManager("test-mesh", ws.NewEventsRoomHandler("global", adminH), adminH)

	router := SetupRouter(RouterConfig{
		Config:         cfg,
		HealthHandler:  healthH,
		ExampleHandler: exampleH,
		RAGHandler:     ragH,
		DB:             nil,
		WSManager:      wsMgr,
		Scheduler:      scheduler,
	})

	t.Run("GET /admin returns embedded HTML", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Hack-Go-Thon Control Center") {
			t.Fatalf("expected HTML to contain dashboard title")
		}
		if !strings.Contains(w.Body.String(), "Scheduled Background Jobs") {
			t.Fatalf("expected HTML to contain Scheduled Background Jobs section")
		}
	})

	t.Run("GET / returns embedded HTML", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "simplysocket Admin") {
			t.Fatalf("expected HTML to contain simplysocket Admin")
		}
	})

	t.Run("GET /api/v1/jobs returns scheduled jobs list", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "sample_cron") {
			t.Fatalf("expected jobs response to contain sample_cron")
		}
	})

	t.Run("POST /api/v1/rag/documents indexes document", func(t *testing.T) {
		payload := map[string]any{
			"title":   "Postgres pgvector",
			"content": "pgvector adds vector similarity search capabilities to PostgreSQL.",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/rag/documents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}
	})

	t.Run("POST /api/v1/rag/ask queries knowledge base", func(t *testing.T) {
		payload := map[string]any{
			"question": "What is pgvector?",
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/rag/ask", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}
