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
	userH := handler.NewUserHandler(nil, cfg.JWTSecret, time.Hour)

	router := SetupRouter(RouterConfig{
		Config:         cfg,
		HealthHandler:  healthH,
		ExampleHandler: exampleH,
		RAGHandler:     ragH,
		WebRTCHandler:  webrtcH,
		UserHandler:    userH,
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

	t.Run("GET /api/v1/users returns seeded users", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/users", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "admin") || !strings.Contains(w.Body.String(), "demo_user") {
			t.Fatalf("expected seeded users in response: %s", w.Body.String())
		}
	})

	t.Run("Multi-Level Auth Enforcement on Protected Routes", func(t *testing.T) {
		// 1. Get demo user token (auth_level 1)
		reqTok, _ := http.NewRequest(http.MethodPost, "/api/v1/users/user_demo_01/token", bytes.NewBuffer([]byte(`{}`)))
		reqTok.Header.Set("Content-Type", "application/json")
		wTok := httptest.NewRecorder()
		router.ServeHTTP(wTok, reqTok)
		if wTok.Code != http.StatusOK {
			t.Fatalf("expected 200 for demo token gen, got %d", wTok.Code)
		}

		var tokData struct {
			Data struct {
				Token string `json:"token"`
			} `json:"data"`
		}
		_ = json.Unmarshal(wTok.Body.Bytes(), &tokData)
		demoToken := tokData.Data.Token

		// Demo token should access standard /profile
		reqProf, _ := http.NewRequest(http.MethodGet, "/api/v1/protected/profile", nil)
		reqProf.Header.Set("Authorization", "Bearer "+demoToken)
		wProf := httptest.NewRecorder()
		router.ServeHTTP(wProf, reqProf)
		if wProf.Code != http.StatusOK {
			t.Fatalf("expected 200 for demo user on /profile, got %d", wProf.Code)
		}

		// Demo token should be FORBIDDEN from /admin-only (requires level 50)
		reqAdmin, _ := http.NewRequest(http.MethodGet, "/api/v1/protected/admin-only", nil)
		reqAdmin.Header.Set("Authorization", "Bearer "+demoToken)
		wAdmin := httptest.NewRecorder()
		router.ServeHTTP(wAdmin, reqAdmin)
		if wAdmin.Code != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for demo user on /admin-only, got %d", wAdmin.Code)
		}

		// 2. Get admin user token (auth_level 99)
		reqAdmTok, _ := http.NewRequest(http.MethodPost, "/api/v1/users/user_admin_01/token", bytes.NewBuffer([]byte(`{}`)))
		reqAdmTok.Header.Set("Content-Type", "application/json")
		wAdmTok := httptest.NewRecorder()
		router.ServeHTTP(wAdmTok, reqAdmTok)
		if wAdmTok.Code != http.StatusOK {
			t.Fatalf("expected 200 for admin token gen, got %d", wAdmTok.Code)
		}
		_ = json.Unmarshal(wAdmTok.Body.Bytes(), &tokData)
		adminToken := tokData.Data.Token

		// Admin token should be allowed on /admin-only
		reqAdminOk, _ := http.NewRequest(http.MethodGet, "/api/v1/protected/admin-only", nil)
		reqAdminOk.Header.Set("Authorization", "Bearer "+adminToken)
		wAdminOk := httptest.NewRecorder()
		router.ServeHTTP(wAdminOk, reqAdminOk)
		if wAdminOk.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for admin user on /admin-only, got %d", wAdminOk.Code)
		}

		// 3. WebSocket Endpoint: Open connection endpoint (room-level auth applies inside simplysocket rooms)
		// Connecting without WebSocket upgrade headers reaches the WS handler and returns 400 Bad Request,
		// confirming it is NOT blocked by route-level 401/403 middleware.
		reqWSNoAuth, _ := http.NewRequest(http.MethodGet, "/api/v1/ws", nil)
		wWSNoAuth := httptest.NewRecorder()
		router.ServeHTTP(wWSNoAuth, reqWSNoAuth)
		if wWSNoAuth.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request (missing WS upgrade headers) for open WS connection, got %d", wWSNoAuth.Code)
		}

		// Level 1 demo user also reaches WS handler (returns 400 due to non-upgrade HTTP request)
		reqWSLevel1, _ := http.NewRequest(http.MethodGet, "/api/v1/ws?token="+demoToken, nil)
		wWSLevel1 := httptest.NewRecorder()
		router.ServeHTTP(wWSLevel1, reqWSLevel1)
		if wWSLevel1.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 Bad Request for level 1 user reaching open WS handler, got %d", wWSLevel1.Code)
		}

		// 4. WebSocket Rooms Telemetry Endpoint
		reqRooms, _ := http.NewRequest(http.MethodGet, "/api/v1/ws/rooms", nil)
		wRooms := httptest.NewRecorder()
		router.ServeHTTP(wRooms, reqRooms)
		if wRooms.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for /api/v1/ws/rooms, got %d", wRooms.Code)
		}
		if !strings.Contains(wRooms.Body.String(), "mesh-global") {
			t.Fatalf("expected rooms response to include 'mesh-global', got %s", wRooms.Body.String())
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
