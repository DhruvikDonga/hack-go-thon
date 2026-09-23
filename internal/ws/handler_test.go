package ws

import (
	"testing"
	"time"

	"hack-go-thon/internal/api/middleware"

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

	t.Run("Admin Room Auth Level strictly above 10", func(t *testing.T) {
		secret := "test-secret"

		// Level 10 (staff) token -> rejected for admin room (requires > 10, i.e. 11 to 99)
		staffToken, err := middleware.GenerateTokenWithMetadata(secret, "user_staff_10", "staff", "staff@test.local", "", "staff", 10, nil, time.Hour)
		if err != nil {
			t.Fatalf("failed to generate staff token: %v", err)
		}
		valid, _, err := middleware.VerifyTokenAuthLevel(secret, staffToken, 11, 99)
		if valid || err == nil {
			t.Fatalf("expected level 10 to be denied entry to admin room (requires above 10)")
		}

		// Level 11 (mod) token -> allowed
		modToken, err := middleware.GenerateTokenWithMetadata(secret, "user_mod_11", "mod", "mod@test.local", "", "moderator", 11, nil, time.Hour)
		if err != nil {
			t.Fatalf("failed to generate mod token: %v", err)
		}
		valid, _, err = middleware.VerifyTokenAuthLevel(secret, modToken, 11, 99)
		if !valid || err != nil {
			t.Fatalf("expected level 11 to be allowed in admin room, got err: %v", err)
		}

		// Level 99 (admin) token -> allowed
		adminToken, err := middleware.GenerateTokenWithMetadata(secret, "user_admin_01", "admin", "admin@test.local", "", "admin", 99, nil, time.Hour)
		if err != nil {
			t.Fatalf("failed to generate admin token: %v", err)
		}
		valid, _, err = middleware.VerifyTokenAuthLevel(secret, adminToken, 11, 99)
		if !valid || err != nil {
			t.Fatalf("expected level 99 to be allowed in admin room, got err: %v", err)
		}
	})
}
