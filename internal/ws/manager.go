package ws

import (
	"hack-go-thon/pkg/log"

	"github.com/DhruvikDonga/simplysocket"
	"github.com/gin-gonic/gin"
)

// Manager coordinates the simplysocket MeshServer and Gin HTTP handler.
// Clients establish a WebSocket connection once, while distinct business logic
// is handled by pluggable RoomData interface implementations.
type Manager struct {
	server        simplysocket.MeshServer
	handler       gin.HandlerFunc
	adminHandler  *AdminRoomHandler
	eventsHandler *EventsRoomHandler
}

// NewManager initializes the WebSocket mesh server with a default RoomData handler and optional AdminRoomHandler.
func NewManager(serverName string, defaultHandler simplysocket.RoomData, adminHandlers ...*AdminRoomHandler) *Manager {
	var admin *AdminRoomHandler
	if len(adminHandlers) > 0 {
		admin = adminHandlers[0]
	}

	var events *EventsRoomHandler
	if defaultHandler == nil {
		events = NewEventsRoomHandler("default-room", admin)
		defaultHandler = events
	} else if eh, ok := defaultHandler.(*EventsRoomHandler); ok {
		events = eh
	}

	config := &simplysocket.MeshServerConfig{
		DirectBroadCast: false,
	}

	// NewMeshServer initializes the internal mesh server instance
	ms := simplysocket.NewMeshServer(serverName, config, defaultHandler)
	log.Info("Initialized simplysocket WebSocket manager", "server", serverName)

	return &Manager{
		server:        ms,
		adminHandler:  admin,
		eventsHandler: events,
		handler: func(c *gin.Context) {
			token := c.Query("token")
			if token == "" {
				token = c.Query("access_token")
			}
			name := c.Query("name")
			if token != "" && name != "" && events != nil {
				events.SetClientToken(name, token)
			}
			simplysocket.ServeWs(ms, c.Writer, c.Request)
		},
	}
}

// Handler returns the Gin handler function for WebSocket upgrade requests.
func (m *Manager) Handler() gin.HandlerFunc {
	return m.handler
}

// Server returns the underlying simplysocket.MeshServer interface.
func (m *Manager) Server() simplysocket.MeshServer {
	return m.server
}

// AdminHandler returns the attached AdminRoomHandler instance if registered.
func (m *Manager) AdminHandler() *AdminRoomHandler {
	return m.adminHandler
}

// EventsHandler returns the attached EventsRoomHandler instance if registered.
func (m *Manager) EventsHandler() *EventsRoomHandler {
	return m.eventsHandler
}

// SetJWTSecret configures the secret used to verify room-level access permissions on the events handler.
func (m *Manager) SetJWTSecret(secret string) {
	if m.eventsHandler != nil {
		m.eventsHandler.SetJWTSecret(secret)
	}
}

// Broadcast sends a server-initiated message to a room.
func (m *Manager) Broadcast(targetRoom, action string, body map[string]any) {
	if targetRoom == "" {
		targetRoom = simplysocket.MeshGlobalRoom
	} else if targetRoom != simplysocket.MeshGlobalRoom {
		rooms := m.server.GetRooms()
		found := false
		for _, r := range rooms {
			if r == targetRoom {
				found = true
				break
			}
		}
		if !found {
			log.Warn("Cannot broadcast to inactive or non-existent room", "room", targetRoom)
			return
		}
	}

	msg := &simplysocket.Message{
		Action:         action,
		Target:         targetRoom,
		MessageBody:    body,
		Sender:         "server",
		IsTargetClient: false,
	}

	select {
	case m.server.PushMessage() <- msg:
		log.Debug("Server dispatched WebSocket broadcast", "room", targetRoom, "action", action)
	default:
		log.Warn("WebSocket push message channel busy", "room", targetRoom)
	}
}

// JoinRoom attaches a client to a room with custom RoomData interface logic.
func (m *Manager) JoinRoom(roomName, clientName string, rd simplysocket.RoomData) {
	m.server.JoinClientRoom(roomName, clientName, rd)
}
