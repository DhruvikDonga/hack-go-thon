package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hack-go-thon/config"
	webrtcserver "hack-go-thon/internal/webrtc_server"

	"github.com/gin-gonic/gin"
	"github.com/pion/webrtc/v4"
)

func TestWebRTCHandler_GetICEServers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		STUNServers:    []string{"stun:stun.l.google.com:19302"},
		TURNServerURL:  "turn:turn.example.com:3478",
		TURNUsername:   "testuser",
		TURNCredential: "testpassword",
	}
	manager := webrtcserver.NewServerPeerManager(cfg)
	h := NewWebRTCHandler(cfg, manager)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest("GET", "/api/v1/webrtc/ice-servers", nil)
	c.Request = req

	h.GetICEServers(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object in response")
	}

	iceServers, ok := data["ice_servers"].([]any)
	if !ok || len(iceServers) != 2 {
		t.Fatalf("expected 2 ice servers, got %v", iceServers)
	}
}

func TestWebRTCHandler_ServerSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		STUNServers: []string{"stun:stun.l.google.com:19302"},
	}
	manager := webrtcserver.NewServerPeerManager(cfg)
	h := NewWebRTCHandler(cfg, manager)

	// 1. Test bad request
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := bytes.NewBufferString(`{}`)
		req := httptest.NewRequest("POST", "/api/v1/webrtc/server/session", body)
		req.Header.Set("Content-Type", "application/json")
		c.Request = req

		h.CreateServerSession(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 bad request, got %d", w.Code)
		}
	}

	// 2. Test successful negotiation
	{
		clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatalf("failed to create client peer connection: %v", err)
		}
		defer clientPC.Close()

		_, _ = clientPC.CreateDataChannel("test-channel", nil)
		offer, err := clientPC.CreateOffer(nil)
		if err != nil {
			t.Fatalf("failed to create offer: %v", err)
		}
		_ = clientPC.SetLocalDescription(offer)

		reqBody, _ := json.Marshal(map[string]string{
			"sdp": offer.SDP,
		})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest("POST", "/api/v1/webrtc/server/session", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req

		h.CreateServerSession(c)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]any)
		sessionID, ok := data["session_id"].(string)
		if !ok || sessionID == "" {
			t.Fatalf("expected non-empty session_id")
		}

		// 3. Test Close session
		wClose := httptest.NewRecorder()
		cClose, _ := gin.CreateTestContext(wClose)
		cClose.Params = gin.Params{{Key: "id", Value: sessionID}}
		reqClose := httptest.NewRequest("DELETE", "/api/v1/webrtc/server/session/"+sessionID, nil)
		cClose.Request = reqClose

		h.CloseServerSession(cClose)
		if wClose.Code != http.StatusOK {
			t.Fatalf("expected 200 OK on close, got %d", wClose.Code)
		}
	}
}
