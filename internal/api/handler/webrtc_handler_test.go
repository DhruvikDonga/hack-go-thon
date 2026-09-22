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
	sfu := webrtcserver.NewSFUEngine(cfg)
	h := NewWebRTCHandler(cfg, manager, sfu)

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
	sfu := webrtcserver.NewSFUEngine(cfg)
	h := NewWebRTCHandler(cfg, manager, sfu)

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

func TestWebRTCHandler_SFU(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		STUNServers: []string{"stun:stun.l.google.com:19302"},
	}
	manager := webrtcserver.NewServerPeerManager(cfg)
	sfu := webrtcserver.NewSFUEngine(cfg)
	h := NewWebRTCHandler(cfg, manager, sfu)

	// 1. Test invalid join request
	{
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest("POST", "/api/v1/webrtc/sfu/join", bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		c.Request = req

		h.JoinSFU(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 bad request, got %d", w.Code)
		}
	}

	// 2. Test valid client join
	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("failed to create client peer connection: %v", err)
	}
	defer clientPC.Close()

	// Add audio track to offer
	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio",
		"pion-stream",
	)
	if err != nil {
		t.Fatalf("failed to create test track: %v", err)
	}
	_, err = clientPC.AddTrack(audioTrack)
	if err != nil {
		t.Fatalf("failed to add track: %v", err)
	}

	offer, err := clientPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("failed to create offer: %v", err)
	}
	_ = clientPC.SetLocalDescription(offer)

	joinReqBody, _ := json.Marshal(webrtcserver.SFUJoinRequest{
		RoomID: "test-room",
		PeerID: "peer-alice",
		SDP:    offer.SDP,
	})

	wJoin := httptest.NewRecorder()
	cJoin, _ := gin.CreateTestContext(wJoin)
	reqJoin := httptest.NewRequest("POST", "/api/v1/webrtc/sfu/join", bytes.NewBuffer(joinReqBody))
	reqJoin.Header.Set("Content-Type", "application/json")
	cJoin.Request = reqJoin

	h.JoinSFU(cJoin)
	if wJoin.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on SFU join, got %d: %s", wJoin.Code, wJoin.Body.String())
	}

	// 3. Test GetSFURooms
	wRooms := httptest.NewRecorder()
	cRooms, _ := gin.CreateTestContext(wRooms)
	reqRooms := httptest.NewRequest("GET", "/api/v1/webrtc/sfu/rooms", nil)
	cRooms.Request = reqRooms

	h.GetSFURooms(cRooms)
	if wRooms.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for SFU rooms, got %d", wRooms.Code)
	}

	// 4. Test GetStatus reports SFU metrics
	wStatus := httptest.NewRecorder()
	cStatus, _ := gin.CreateTestContext(wStatus)
	reqStatus := httptest.NewRequest("GET", "/api/v1/webrtc/status", nil)
	cStatus.Request = reqStatus

	h.GetStatus(cStatus)
	if wStatus.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for status, got %d", wStatus.Code)
	}

	var statusResp map[string]any
	_ = json.Unmarshal(wStatus.Body.Bytes(), &statusResp)
	statusData := statusResp["data"].(map[string]any)
	if activeRooms, ok := statusData["sfu_active_rooms"].(float64); !ok || activeRooms < 1 {
		t.Fatalf("expected at least 1 active sfu room in status, got %v", statusData["sfu_active_rooms"])
	}

	// 5. Test LeaveSFU
	wLeave := httptest.NewRecorder()
	cLeave, _ := gin.CreateTestContext(wLeave)
	cLeave.Params = gin.Params{
		{Key: "room_id", Value: "test-room"},
		{Key: "peer_id", Value: "peer-alice"},
	}
	reqLeave := httptest.NewRequest("POST", "/api/v1/webrtc/sfu/leave", nil)
	cLeave.Request = reqLeave

	h.LeaveSFU(cLeave)
	if wLeave.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for SFU leave, got %d", wLeave.Code)
	}
}
