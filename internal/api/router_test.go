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
	webrtcserver "hack-go-thon/internal/webrtc_server"
	"hack-go-thon/internal/ws"
)

func TestRouter_AdminAndRAG(t *testing.T) {
	cfg := &config.Config{
		Environment:    "test",
		AllowedOrigins: []string{"*"},
		JWTSecret:      "test-secret",
		STUNServers:    []string{"stun:stun.l.google.com:19302"},
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
	webrtcMgr := webrtcserver.NewServerPeerManager(cfg)
	sfuEngine := webrtcserver.NewSFUEngine(cfg)
	webrtcH := handler.NewWebRTCHandler(cfg, webrtcMgr, sfuEngine)

	router := SetupRouter(RouterConfig{
		Config:         cfg,
		HealthHandler:  healthH,
		ExampleHandler: exampleH,
		RAGHandler:     ragH,
		WebRTCHandler:  webrtcH,
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

	t.Run("GET /api/v1/webrtc/ice-servers returns STUN servers", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/webrtc/ice-servers", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "stun:stun.l.google.com:19302") {
			t.Fatalf("expected STUN server in response")
		}
	})

	t.Run("GET /api/v1/webrtc/status returns status", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/webrtc/status", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "active_server_sessions") {
			t.Fatalf("expected active_server_sessions in response")
		}
	})
}

func TestRouter_DisabledServices(t *testing.T) {
	cfg := &config.Config{
		Environment:    "test",
		AllowedOrigins: []string{"*"},
		JWTSecret:      "test-secret",
	}

	healthH := handler.NewHealthHandler()

	// All optional handlers are nil, simulating services.json with false for everything
	router := SetupRouter(RouterConfig{
		Config:         cfg,
		HealthHandler:  healthH,
		ExampleHandler: nil,
		RAGHandler:     nil,
		WebRTCHandler:  nil,
		DB:             nil,
		WSManager:      nil,
		Scheduler:      nil,
	})

	t.Run("Health check still works when all services disabled", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Admin dashboard UI is still accessible", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/admin", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Disabled WS endpoint returns 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/ws", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for disabled WS, got %d", w.Code)
		}
	})

	t.Run("Disabled WebRTC endpoint returns 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/webrtc/status", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for disabled WebRTC, got %d", w.Code)
		}
	})

	t.Run("Disabled Jobs endpoint returns 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for disabled Jobs, got %d", w.Code)
		}
	})

	t.Run("Disabled RAG endpoint returns 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/rag/ask", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for disabled RAG, got %d", w.Code)
		}
	})

	t.Run("Disabled Items endpoint returns 404", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/items", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for disabled Items, got %d", w.Code)
		}
	})
}
