package ws

import (
	"testing"
)

func TestWebRTCRoomHandler_Basics(t *testing.T) {
	handler := NewWebRTCRoomHandler("call-room-1")
	if handler.Slug() != "call-room-1" {
		t.Fatalf("expected slug 'call-room-1', got %q", handler.Slug())
	}

	defaultHandler := NewWebRTCRoomHandler("")
	if defaultHandler.Slug() != "webrtc" {
		t.Fatalf("expected default slug 'webrtc', got %q", defaultHandler.Slug())
	}

	peers := handler.GetPeers()
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers initially, got %d", len(peers))
	}
}
