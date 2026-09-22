package ws

import (
	"testing"
)

func TestAdminRoomHandler(t *testing.T) {
	handler := NewAdminRoomHandler(nil)
	if handler.Slug() != "admin" {
		t.Fatalf("expected slug 'admin', got %q", handler.Slug())
	}
}
