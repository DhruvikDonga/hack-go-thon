package webrtcserver

import (
	"context"
	"testing"
	"time"

	"hack-go-thon/config"

	"github.com/pion/webrtc/v4"
)

func TestServerPeerManager_Lifecycle(t *testing.T) {
	cfg := &config.Config{
		STUNServers: []string{"stun:stun.l.google.com:19302"},
	}

	manager := NewServerPeerManager(cfg)
	if manager.ActiveSessionsCount() != 0 {
		t.Fatalf("expected 0 active sessions, got %d", manager.ActiveSessionsCount())
	}

	// Create a client PeerConnection to generate a real SDP offer
	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("failed to create client peer connection: %v", err)
	}
	defer clientPC.Close()

	// Add a DataChannel to negotiate
	_, err = clientPC.CreateDataChannel("test-channel", nil)
	if err != nil {
		t.Fatalf("failed to create data channel: %v", err)
	}

	offer, err := clientPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("failed to create offer: %v", err)
	}

	err = clientPC.SetLocalDescription(offer)
	if err != nil {
		t.Fatalf("failed to set local description: %v", err)
	}

	// Handle offer on server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := manager.HandleOffer(ctx, offer.SDP)
	if err != nil {
		t.Fatalf("HandleOffer failed: %v", err)
	}

	if resp.SessionID == "" {
		t.Error("expected non-empty session ID")
	}
	if resp.Answer.SDP == "" {
		t.Error("expected non-empty answer SDP")
	}
	if resp.Answer.Type != "answer" {
		t.Errorf("expected answer type 'answer', got %q", resp.Answer.Type)
	}

	if manager.ActiveSessionsCount() != 1 {
		t.Errorf("expected 1 active session, got %d", manager.ActiveSessionsCount())
	}

	// Close session
	manager.CloseSession(resp.SessionID)
	if manager.ActiveSessionsCount() != 0 {
		t.Errorf("expected 0 active sessions after close, got %d", manager.ActiveSessionsCount())
	}
}
