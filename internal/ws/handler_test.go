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
}
