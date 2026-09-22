package ws

import (
	"time"

	"hack-go-thon/pkg/log"

	"github.com/DhruvikDonga/simplysocket"
)

// ChatRoomHandler demonstrates a second specialized RoomData implementation
// that handles group chat interactions over the same WebSocket connection.
type ChatRoomHandler struct {
	slug string
}

// NewChatRoomHandler creates an instance of ChatRoomHandler.
func NewChatRoomHandler(slug string) *ChatRoomHandler {
	return &ChatRoomHandler{
		slug: slug,
	}
}

// Slug returns the chat room identifier.
func (c *ChatRoomHandler) Slug() string {
	return c.slug
}

// HandleRoomData implements simplysocket.RoomData for interactive chat rooms.
func (c *ChatRoomHandler) HandleRoomData(room simplysocket.Room, server simplysocket.MeshServer) {
	roomName := room.GetRoomSlugInfo()
	log.Info("ChatRoomHandler started for room", "room", roomName)

	for {
		select {
		case msg, ok := <-room.ConsumeRoomMessage():
			if !ok {
				log.Info("Chat room message stream ended", "room", roomName)
				return
			}

			if msg.Target != roomName {
				continue
			}

			switch msg.Action {
			case "send-chat":
				// Augment message with server timestamp
				if msg.MessageBody == nil {
					msg.MessageBody = make(map[string]any)
				}
				msg.MessageBody["server_time"] = time.Now().UTC().Format(time.RFC3339)
				msg.IsTargetClient = false

				// Broadcast chat message to everyone in room
				room.BroadcastMessage(msg)

			case "user-typing":
				// Broadcast typing status to room
				room.BroadcastMessage(msg)
			}

		case event := <-room.EventTriggers():
			if len(event) >= 3 {
				log.Info("Chat room event", "event", event[0], "room", event[1], "client", event[2])
			}

		case <-room.RoomStopped():
			log.Info("Chat room terminated", "room", roomName)
			return
		}
	}
}
