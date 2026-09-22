package ws

import (
	"sync"
	"time"

	"hack-go-thon/pkg/log"

	"github.com/DhruvikDonga/simplysocket"
)

// RoomHandler defines the interface for modular room business logic.
// By implementing this, developers can write different logic handlers
// while reusing a single established WebSocket connection.
type RoomHandler interface {
	simplysocket.RoomData
	Slug() string
}

// EventsRoomHandler is an extensible reference implementation of simplysocket.RoomData.
type EventsRoomHandler struct {
	slug         string
	adminHandler *AdminRoomHandler
	mu           sync.RWMutex
}

// NewEventsRoomHandler creates a new EventsRoomHandler with the given room slug and optional admin handler.
func NewEventsRoomHandler(slug string, adminHandlers ...*AdminRoomHandler) *EventsRoomHandler {
	var admin *AdminRoomHandler
	if len(adminHandlers) > 0 {
		admin = adminHandlers[0]
	}
	return &EventsRoomHandler{
		slug:         slug,
		adminHandler: admin,
	}
}

// Slug returns the room slug identifier.
func (h *EventsRoomHandler) Slug() string {
	return h.slug
}

// HandleRoomData implements simplysocket.RoomData.
// This loop handles incoming client messages, room connection events, and room termination.
func (h *EventsRoomHandler) HandleRoomData(room simplysocket.Room, server simplysocket.MeshServer) {
	roomName := room.GetRoomSlugInfo()
	log.Info("Initialized WebSocket RoomData handler",
		"room", roomName,
		"server", server.GetGameName(),
	)

	// Launch periodic telemetry / heartbeat broadcaster for this room
	h.startHeartbeat(room)

	for {
		select {
		case msg, ok := <-room.ConsumeRoomMessage():
			if !ok {
				log.Info("Room message channel closed", "room", roomName)
				return
			}

			log.Debug("WebSocket message received in room",
				"room", roomName,
				"sender", msg.Sender,
				"action", msg.Action,
			)

			// Process message actions
			switch msg.Action {
			case "join-room":
				targetRoom, _ := msg.MessageBody["room"].(string)
				if targetRoom == "" {
					targetRoom = "admin"
				}

				var rd simplysocket.RoomData
				switch targetRoom {
				case "admin":
					if h.adminHandler != nil {
						rd = h.adminHandler
					} else {
						rd = NewAdminRoomHandler(nil)
					}
				case "chat":
					rd = NewChatRoomHandler("chat")
				default:
					rd = NewEventsRoomHandler(targetRoom, h.adminHandler)
				}

				server.JoinClientRoom(targetRoom, msg.Sender, rd)
				log.Info("Client joined room", "client", msg.Sender, "room", targetRoom)

				// Acknowledge back to client
				room.BroadcastMessage(&simplysocket.Message{
					Action: "joined-room-ack",
					Target: roomName,
					MessageBody: map[string]any{
						"status":      "success",
						"joined_room": targetRoom,
						"client_slug": msg.Sender,
						"time":        time.Now().UTC().Format(time.RFC3339),
					},
					Sender:         "server",
					IsTargetClient: false,
				})

			case "chat-message", "broadcast":
				// Re-broadcast message to all clients in this room
				msg.IsTargetClient = false
				room.BroadcastMessage(msg)

			case "ping":
				// Echo pong back to the sender client
				pongMsg := &simplysocket.Message{
					Action: "pong",
					Target: roomName,
					MessageBody: map[string]any{
						"time": time.Now().UTC().Format(time.RFC3339),
					},
					Sender:         "server",
					IsTargetClient: true,
				}
				room.BroadcastMessage(pongMsg)
			}

		case clientEvent := <-room.EventTriggers():
			if len(clientEvent) >= 3 {
				eventType := clientEvent[0]
				eventRoom := clientEvent[1]
				clientID := clientEvent[2]

				log.Info("WebSocket client event triggered",
					"event", eventType,
					"room", eventRoom,
					"client_id", clientID,
				)

				// Notify other room occupants about join/leave
				room.BroadcastMessage(&simplysocket.Message{
					Action: "user-event",
					Target: roomName,
					MessageBody: map[string]any{
						"event":     eventType,
						"client_id": clientID,
						"time":      time.Now().UTC().Format(time.RFC3339),
					},
					Sender:         "system",
					IsTargetClient: false,
				})
			}

		case <-room.RoomStopped():
			log.Info("WebSocket room stopped, terminating handler loop", "room", roomName)
			return
		}
	}
}

func (h *EventsRoomHandler) startHeartbeat(room simplysocket.Room) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		roomName := room.GetRoomSlugInfo()

		for {
			select {
			case <-ticker.C:
				room.BroadcastMessage(&simplysocket.Message{
					Action: "heartbeat",
					Target: roomName,
					MessageBody: map[string]any{
						"status": "alive",
						"time":   time.Now().UTC().Format(time.RFC3339),
					},
					Sender:         "server",
					IsTargetClient: false,
				})

			case <-room.RoomStopped():
				return
			}
		}
	}()
}
