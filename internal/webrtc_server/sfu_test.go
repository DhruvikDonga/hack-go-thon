package webrtcserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hack-go-thon/config"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

func TestSFUEngine_RoomCreation(t *testing.T) {
	cfg := &config.Config{
		STUNServers: []string{"stun:stun.l.google.com:19302"},
	}
	engine := NewSFUEngine(cfg)

	room1 := engine.GetOrCreateRoom("room-alpha")
	if room1 == nil || room1.ID != "room-alpha" {
		t.Fatalf("expected room ID 'room-alpha', got %v", room1)
	}

	// Calling GetOrCreateRoom again must return the identical instance
	room1Again := engine.GetOrCreateRoom("room-alpha")
	if room1 != room1Again {
		t.Fatalf("expected identical room instance")
	}

	summaries := engine.GetRoomsSummary()
	if len(summaries) != 1 || summaries[0].ID != "room-alpha" {
		t.Fatalf("expected 1 summary for room-alpha, got %+v", summaries)
	}
}

func TestSFUEngine_JoinAndLeave(t *testing.T) {
	cfg := &config.Config{
		STUNServers: []string{"stun:stun.l.google.com:19302"},
	}
	engine := NewSFUEngine(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Create client 1 PeerConnection with audio track
	pc1, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("failed to create client 1 PC: %v", err)
	}
	defer pc1.Close()

	track1, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio-1",
		"stream-1",
	)
	if err != nil {
		t.Fatalf("failed to create track1: %v", err)
	}
	if _, err := pc1.AddTrack(track1); err != nil {
		t.Fatalf("failed to add track1: %v", err)
	}

	offer1, err := pc1.CreateOffer(nil)
	if err != nil {
		t.Fatalf("failed to create offer1: %v", err)
	}
	if err := pc1.SetLocalDescription(offer1); err != nil {
		t.Fatalf("failed to set local description 1: %v", err)
	}

	// 2. Peer 1 joins room
	resp1, err := engine.JoinRoom(ctx, SFUJoinRequest{
		RoomID: "conf-main",
		PeerID: "alice",
		SDP:    offer1.SDP,
	})
	if err != nil {
		t.Fatalf("failed to join room as alice: %v", err)
	}
	if resp1.Status != "connected" || resp1.ActivePeers != 1 {
		t.Fatalf("unexpected join response: %+v", resp1)
	}

	// 3. Create client 2 PeerConnection with video track
	pc2, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("failed to create client 2 PC: %v", err)
	}
	defer pc2.Close()

	track2, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"video-2",
		"stream-2",
	)
	if err != nil {
		t.Fatalf("failed to create track2: %v", err)
	}
	if _, err := pc2.AddTrack(track2); err != nil {
		t.Fatalf("failed to add track2: %v", err)
	}

	offer2, err := pc2.CreateOffer(nil)
	if err != nil {
		t.Fatalf("failed to create offer2: %v", err)
	}
	if err := pc2.SetLocalDescription(offer2); err != nil {
		t.Fatalf("failed to set local description 2: %v", err)
	}

	// 4. Peer 2 joins room
	resp2, err := engine.JoinRoom(ctx, SFUJoinRequest{
		RoomID: "conf-main",
		PeerID: "bob",
		SDP:    offer2.SDP,
	})
	if err != nil {
		t.Fatalf("failed to join room as bob: %v", err)
	}
	if resp2.Status != "connected" || resp2.ActivePeers != 2 {
		t.Fatalf("unexpected join response for bob: %+v", resp2)
	}

	// Check rooms summary
	summaries := engine.GetRoomsSummary()
	if len(summaries) != 1 {
		t.Fatalf("expected 1 room, got %d", len(summaries))
	}
	if summaries[0].PeerCount != 2 {
		t.Fatalf("expected 2 peers in room, got %d", summaries[0].PeerCount)
	}

	// 5. Renegotiate test for alice
	renegOffer, err := pc1.CreateOffer(nil)
	if err != nil {
		t.Fatalf("failed to create reneg offer: %v", err)
	}
	renegAnswer, err := engine.Renegotiate(ctx, "conf-main", "alice", renegOffer.SDP)
	if err != nil {
		t.Fatalf("failed to renegotiate alice: %v", err)
	}
	if renegAnswer.Type != "answer" || renegAnswer.SDP == "" {
		t.Fatalf("invalid renegotiation answer: %+v", renegAnswer)
	}

	// 6. Non-existent peer renegotiation must error
	_, err = engine.Renegotiate(ctx, "conf-main", "non-existent", renegOffer.SDP)
	if err == nil {
		t.Fatalf("expected error for non-existent peer renegotiation")
	}

	// 7. Alice leaves
	engine.LeaveRoom("conf-main", "alice")
	summaries = engine.GetRoomsSummary()
	if len(summaries) != 1 || summaries[0].PeerCount != 1 {
		t.Fatalf("expected 1 peer remaining after alice leaves, got %+v", summaries)
	}

	// 8. Bob leaves - room should be removed
	engine.LeaveRoom("conf-main", "bob")
	summaries = engine.GetRoomsSummary()
	if len(summaries) != 0 {
		t.Fatalf("expected 0 rooms after all peers leave, got %+v", summaries)
	}
}

func TestSFUEngine_MultiPartyTrackFanOut(t *testing.T) {
	cfg := &config.Config{}
	engine := NewSFUEngine(cfg)
	room := engine.GetOrCreateRoom("multi-party-test")

	// Create Peer 1
	pc1, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("failed to create PC1: %v", err)
	}
	defer pc1.Close()

	peer1 := &SFUPeer{
		ID:        "peer1",
		RoomID:    "multi-party-test",
		PC:        pc1,
		senders:   make(map[string]*webrtc.RTPSender),
		closeChan: make(chan struct{}),
	}

	// Create Peer 2
	pc2, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("failed to create PC2: %v", err)
	}
	defer pc2.Close()

	peer2 := &SFUPeer{
		ID:        "peer2",
		RoomID:    "multi-party-test",
		PC:        pc2,
		senders:   make(map[string]*webrtc.RTPSender),
		closeChan: make(chan struct{}),
	}

	room.mu.Lock()
	room.peers["peer1"] = peer1
	room.peers["peer2"] = peer2
	room.mu.Unlock()

	// Peer 1 publishes video track
	localTrack1, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"peer1_video",
		"peer1",
	)
	if err != nil {
		t.Fatalf("failed to create localTrack1: %v", err)
	}

	sfuTrack1 := &SFUTrack{
		ID:        "peer1_video",
		Kind:      "video",
		Publisher: "peer1",
		Track:     localTrack1,
	}

	room.mu.Lock()
	room.tracks["peer1_video"] = sfuTrack1
	room.signalPeerConnectionsLocked()
	room.mu.Unlock()

	// Verify Peer 2 has the track from Peer 1, and Peer 1 has 0 senders (not sending to itself)
	peer2.mu.RLock()
	if _, ok := peer2.senders["peer1_video"]; !ok {
		t.Errorf("expected peer2 to receive peer1_video track")
	}
	peer2.mu.RUnlock()

	peer1.mu.RLock()
	if len(peer1.senders) != 0 {
		t.Errorf("expected peer1 to have 0 senders, got %d", len(peer1.senders))
	}
	peer1.mu.RUnlock()

	// Call signalPeerConnectionsLocked AGAIN and verify the track is NOT removed!
	room.mu.Lock()
	room.signalPeerConnectionsLocked()
	room.mu.Unlock()

	peer2.mu.RLock()
	if _, ok := peer2.senders["peer1_video"]; !ok {
		t.Errorf("track was unexpectedly removed from peer2 on subsequent sync!")
	}
	peer2.mu.RUnlock()

	// Peer 2 publishes video track
	localTrack2, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"peer2_video",
		"peer2",
	)
	if err != nil {
		t.Fatalf("failed to create localTrack2: %v", err)
	}

	sfuTrack2 := &SFUTrack{
		ID:        "peer2_video",
		Kind:      "video",
		Publisher: "peer2",
		Track:     localTrack2,
	}

	room.mu.Lock()
	room.tracks["peer2_video"] = sfuTrack2
	room.signalPeerConnectionsLocked()
	room.mu.Unlock()

	// Both peers should now have each other's track
	peer1.mu.RLock()
	if _, ok := peer1.senders["peer2_video"]; !ok {
		t.Errorf("expected peer1 to receive peer2_video track")
	}
	peer1.mu.RUnlock()

	peer2.mu.RLock()
	if _, ok := peer2.senders["peer1_video"]; !ok {
		t.Errorf("expected peer2 to still have peer1_video track")
	}
	peer2.mu.RUnlock()

	// Peer 1 leaves room: verify peer1_video is cleanly removed from peer2's senders
	engine.LeaveRoom("multi-party-test", "peer1")

	peer2.mu.RLock()
	if _, ok := peer2.senders["peer1_video"]; ok {
		t.Errorf("expected peer1_video to be removed from peer2 after peer1 left")
	}
	peer2.mu.RUnlock()
}

func TestSFUEngine_WebSocketRenegotiationPending(t *testing.T) {
	cfg := &config.Config{
		STUNServers: []string{},
	}
	engine := NewSFUEngine(cfg)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		engine.HandleWebSocket(w, r, "reneg-test-room", "client1")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer ws.Close()

	readOffer := func() string {
		for {
			_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, raw, err := ws.ReadMessage()
			if err != nil {
				t.Fatalf("failed to read ws message: %v", err)
			}
			var msg struct {
				Event string `json:"event"`
				Data  string `json:"data"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				t.Fatalf("failed to unmarshal ws message: %v", err)
			}
			if msg.Event == "offer" {
				return msg.Data
			}
		}
	}

	sendAnswer := func(clientPC *webrtc.PeerConnection, offerSDP string) {
		var offer webrtc.SessionDescription
		if err := json.Unmarshal([]byte(offerSDP), &offer); err != nil {
			t.Fatalf("failed to parse offer SDP: %v", err)
		}
		if err := clientPC.SetRemoteDescription(offer); err != nil {
			t.Fatalf("client failed to set remote description: %v", err)
		}
		ans, err := clientPC.CreateAnswer(nil)
		if err != nil {
			t.Fatalf("client failed to create answer: %v", err)
		}
		if err := clientPC.SetLocalDescription(ans); err != nil {
			t.Fatalf("client failed to set local description: %v", err)
		}
		ansBytes, _ := json.Marshal(ans)
		msg := map[string]any{
			"event": "answer",
			"data":  string(ansBytes),
		}
		_ = ws.WriteJSON(msg)
	}

	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("failed to create client PC: %v", err)
	}
	defer clientPC.Close()

	// 1. Initial offer
	initialOffer := readOffer()
	sendAnswer(clientPC, initialOffer)

	// Wait briefly for server to process initial answer
	time.Sleep(100 * time.Millisecond)

	// 2. User 2 adds track 1
	room := engine.GetOrCreateRoom("reneg-test-room")
	localTrack1, _ := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8},
		"track1",
		"peer2",
	)
	sfuTrack1 := &SFUTrack{
		ID:        "track1",
		Kind:      "video",
		Publisher: "peer2",
		Track:     localTrack1,
	}

	room.mu.Lock()
	room.tracks["track1"] = sfuTrack1
	room.signalPeerConnectionsLocked()
	room.mu.Unlock()

	// Read offer for track 1
	offer1 := readOffer()

	// 3. User 2 adds track 2 while client1 is in HaveLocalOffer!
	localTrack2, _ := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"track2",
		"peer2",
	)
	sfuTrack2 := &SFUTrack{
		ID:        "track2",
		Kind:      "audio",
		Publisher: "peer2",
		Track:     localTrack2,
	}

	room.mu.Lock()
	room.tracks["track2"] = sfuTrack2
	room.signalPeerConnectionsLocked()
	room.mu.Unlock()

	// Verify renegotiatePending is set
	room.mu.RLock()
	peer1 := room.peers["client1"]
	if !peer1.renegotiatePending {
		t.Errorf("expected renegotiatePending to be true")
	}
	room.mu.RUnlock()

	// 4. Client answers offer 1
	sendAnswer(clientPC, offer1)

	// 5. Server should now detect renegotiatePending on answer and send offer 2
	offer2 := readOffer()

	// 6. Client answers offer 2
	sendAnswer(clientPC, offer2)

	// Wait for server to process answer 2
	time.Sleep(100 * time.Millisecond)

	room.mu.RLock()
	if peer1.renegotiatePending {
		t.Errorf("expected renegotiatePending to be false after second answer")
	}
	if _, ok := peer1.senders["track1"]; !ok {
		t.Errorf("expected peer1 to have track1")
	}
	if _, ok := peer1.senders["track2"]; !ok {
		t.Errorf("expected peer1 to have track2")
	}
	room.mu.RUnlock()
}
