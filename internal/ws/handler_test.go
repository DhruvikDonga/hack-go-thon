package ws

import (
	"testing"

	"github.com/DhruvikDonga/simplysocket"
)

func TestWebSocketHandlers(t *testing.T) {
	t.Run("EventsRoomHandler Slug", func(t *testing.T) {
		h := NewEventsRoomHandler("events-room")
		if h.Slug() != "events-room" {
			t.Errorf("expected slug 'events-room', got '%s'", h.Slug())
		}
	})

	t.Run("ChatRoomHandler Slug", func(t *testing.T) {
		h := NewChatRoomHandler("general-chat")
		if h.Slug() != "general-chat" {
			t.Errorf("expected slug 'general-chat', got '%s'", h.Slug())
		}
	})

	t.Run("Manager Initialization", func(t *testing.T) {
		m := NewManager("test-mesh", nil)
		if m == nil {
			t.Fatalf("expected non-nil manager")
		}
		if m.Server() == nil {
			t.Errorf("expected non-nil mesh server")
		}
		if m.Handler() == nil {
			t.Errorf("expected non-nil gin handler func")
		}

		// Test broadcast dispatch to the default global room
		m.Broadcast(simplysocket.MeshGlobalRoom, "test-action", map[string]any{"key": "value"})
	})

	t.Run("EventsRoomHandler ClientToken and JWTSecret", func(t *testing.T) {
		h := NewEventsRoomHandler("events-room")
		h.SetJWTSecret("my-secret")
		h.SetClientToken("client-123", "tok-xyz")

		if tok := h.GetClientToken("client-123"); tok != "tok-xyz" {
			t.Errorf("expected 'tok-xyz', got '%s'", tok)
		}
		if tok := h.GetClientToken("non-existent"); tok != "" {
			t.Errorf("expected empty string for non-existent client, got '%s'", tok)
		}
	})

	t.Run("Manager Token & JWT Configuration", func(t *testing.T) {
		h := NewEventsRoomHandler("global")
		m := NewManager("test-mesh", h)
		m.SetJWTSecret("super-secret")
		if m.EventsHandler() == nil {
			t.Fatalf("expected non-nil EventsHandler")
		}
		m.EventsHandler().SetClientToken("alice", "token-alice")
		if tok := m.EventsHandler().GetClientToken("alice"); tok != "token-alice" {
			t.Errorf("expected 'token-alice', got '%s'", tok)
		}
	})
}
