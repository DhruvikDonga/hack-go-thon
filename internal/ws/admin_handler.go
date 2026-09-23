package ws

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"hack-go-thon/internal/jobs"
	llmclient "hack-go-thon/internal/llm_client"
	"hack-go-thon/pkg/log"
	"sort"

	"github.com/DhruvikDonga/simplysocket"
	"github.com/google/uuid"
)

// AdminRoomHandler provides full observability and management capabilities over simplysocket.
// Connected clients in the "admin" room receive real-time mesh topology updates (rooms, clients, events),
// background scheduled jobs status, can execute LLM token streaming over WebSockets, and can broadcast announcements.
type AdminRoomHandler struct {
	slug      string
	llm       *llmclient.LLMClient
	scheduler *jobs.Scheduler
	mu        sync.RWMutex
}

// NewAdminRoomHandler initializes a new AdminRoomHandler with optional LLM client and job scheduler.
func NewAdminRoomHandler(llm *llmclient.LLMClient, schedulers ...*jobs.Scheduler) *AdminRoomHandler {
	var sched *jobs.Scheduler
	if len(schedulers) > 0 {
		sched = schedulers[0]
	}
	return &AdminRoomHandler{
		slug:      "admin",
		llm:       llm,
		scheduler: sched,
	}
}

// Slug returns the room identifier.
func (h *AdminRoomHandler) Slug() string {
	return h.slug
}

// SetScheduler attaches a Job Scheduler to the admin handler for live job observability.
func (h *AdminRoomHandler) SetScheduler(s *jobs.Scheduler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.scheduler = s
}

// HandleRoomData implements simplysocket.RoomData for the admin room.
func (h *AdminRoomHandler) HandleRoomData(room simplysocket.Room, server simplysocket.MeshServer) {
	roomName := room.GetRoomSlugInfo()
	log.Info("AdminRoomHandler started for room", "room", roomName)

	// Periodic mesh state broadcaster
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Helper to build and broadcast full mesh snapshot
	broadcastState := func() {
		h.mu.RLock()
		sched := h.scheduler
		h.mu.RUnlock()

		state := buildMeshSnapshot(server, sched)
		room.BroadcastMessage(&simplysocket.Message{
			Action:         "admin-state",
			Target:         roomName,
			MessageBody:    state,
			Sender:         "server",
			IsTargetClient: false,
		})
	}

	// Immediately push initial state upon startup
	broadcastState()

	for {
		select {
		case msg, ok := <-room.ConsumeRoomMessage():
			if !ok {
				log.Info("Admin room message stream closed", "room", roomName)
				return
			}

			if msg.Target != roomName {
				continue
			}

			log.Debug("Admin room received message", "action", msg.Action, "sender", msg.Sender)

			switch msg.Action {
			case "get-admin-state":
				broadcastState()

			case "admin-broadcast":
				// Admin wants to broadcast a global or room-specific announcement
				targetRoom, _ := msg.MessageBody["target_room"].(string)
				if targetRoom == "" {
					targetRoom = simplysocket.MeshGlobalRoom
				}
				text, _ := msg.MessageBody["message"].(string)
				if text == "" {
					text = "System alert from Admin"
				}
				// Validate that target room exists in mesh to prevent simplysocket panic on nil room
				activeRooms := server.GetRooms()
				roomExists := false
				for _, r := range activeRooms {
					if r == targetRoom {
						roomExists = true
						break
					}
				}

				if !roomExists {
					log.Warn("Admin broadcast rejected: target room is not active", "target_room", targetRoom)
					room.BroadcastMessage(&simplysocket.Message{
						Action: "broadcast-error",
						Target: roomName,
						MessageBody: map[string]any{
							"error":       fmt.Sprintf("Target room %q is not active or has no connected clients", targetRoom),
							"target_room": targetRoom,
						},
						Sender:         "server",
						IsTargetClient: false,
					})
					continue
				}

				senderName := msg.Sender
				if fromUser, ok := msg.MessageBody["sender_username"].(string); ok && fromUser != "" {
					senderName = fromUser
				} else if fromUser, ok := msg.MessageBody["from"].(string); ok && fromUser != "" {
					senderName = fromUser
				}

				announcement := &simplysocket.Message{
					Action: "system-announcement",
					Target: targetRoom,
					MessageBody: map[string]any{
						"announcement":    text,
						"message":         text,
						"from":            senderName,
						"sender":          senderName,
						"sender_username": senderName,
						"target_room":     targetRoom,
						"time":            time.Now().UTC().Format(time.RFC3339),
					},
					Sender:         senderName,
					IsTargetClient: false,
				}

				select {
				case server.PushMessage() <- announcement:
					log.Info("Admin broadcast sent", "target", targetRoom, "sender", senderName, "message", text)
				default:
					log.Warn("Failed to push admin announcement, channel full")
				}

				// Acknowledge in admin room
				room.BroadcastMessage(&simplysocket.Message{
					Action: "broadcast-ack",
					Target: roomName,
					MessageBody: map[string]any{
						"status":      "dispatched",
						"target_room": targetRoom,
						"message":     text,
					},
					Sender:         "server",
					IsTargetClient: false,
				})

			case "join-room":
				targetRoom, _ := msg.MessageBody["room"].(string)
				if targetRoom == "" {
					targetRoom = "admin"
				}

				var rd simplysocket.RoomData
				switch targetRoom {
				case "admin":
					rd = h
				case "chat":
					rd = NewChatRoomHandler("chat")
				default:
					if strings.HasPrefix(targetRoom, "call-") || strings.HasPrefix(targetRoom, "webrtc") {
						rd = NewWebRTCRoomHandler(targetRoom)
					} else {
						rd = NewEventsRoomHandler(targetRoom, h)
					}
				}

				server.JoinClientRoom(targetRoom, msg.Sender, rd)
				log.Info("Client joined room via admin handler", "client", msg.Sender, "room", targetRoom)

				// Acknowledge back to client specifically in admin room
				room.BroadcastMessage(&simplysocket.Message{
					Action: "joined-room-ack",
					Target: msg.Sender,
					MessageBody: map[string]any{
						"status":      "success",
						"joined_room": targetRoom,
						"client_slug": msg.Sender,
						"time":        time.Now().UTC().Format(time.RFC3339),
					},
					Sender:         "server",
					IsTargetClient: true,
				})

			case "llm-stream-request":
				// Real-time LLM token streaming over simplysocket
				prompt, _ := msg.MessageBody["prompt"].(string)
				model, _ := msg.MessageBody["model"].(string)
				streamID, _ := msg.MessageBody["stream_id"].(string)
				if streamID == "" {
					streamID = "stream_" + uuid.New().String()[:8]
				}

				go h.handleLLMStream(room, roomName, msg.Sender, streamID, model, prompt)

			case "webrtc-join", "webrtc-offer", "webrtc-answer", "webrtc-ice", "webrtc-leave":
				// Forward WebRTC signaling messages to peers in admin room
				msg.IsTargetClient = false
				room.BroadcastMessage(msg)

			case "system-announcement", "chat-message", "broadcast", "send-chat", "message":
				// Re-broadcast announcements or chat messages to all clients in this room
				if fromUser, ok := msg.MessageBody["sender_username"].(string); ok && fromUser != "" {
					msg.Sender = fromUser
				} else if fromUser, ok := msg.MessageBody["from"].(string); ok && fromUser != "" {
					msg.Sender = fromUser
				}
				msg.IsTargetClient = false
				room.BroadcastMessage(msg)
			}

		case clientEvent := <-room.EventTriggers():
			if len(clientEvent) >= 3 {
				log.Info("Admin room client event",
					"event", clientEvent[0],
					"room", clientEvent[1],
					"client", clientEvent[2],
				)
				// Broadcast updated snapshot whenever someone joins or leaves admin room
				broadcastState()
			}

		case <-ticker.C:
			// Regular heartbeat snapshot to keep connected admin dashboards current
			broadcastState()

		case <-room.RoomStopped():
			log.Info("Admin room stopped", "room", roomName)
			return
		}
	}
}

func (h *AdminRoomHandler) handleLLMStream(room simplysocket.Room, roomName, senderSlug, streamID, model, prompt string) {
	if prompt == "" {
		prompt = "Hello! Please summarize what you can do."
	}

	// 1. Send stream start event
	room.BroadcastMessage(&simplysocket.Message{
		Action: "llm-stream-start",
		Target: roomName,
		MessageBody: map[string]any{
			"stream_id": streamID,
			"prompt":    prompt,
			"sender":    senderSlug,
			"time":      time.Now().UTC().Format(time.RFC3339),
		},
		Sender:         "server",
		IsTargetClient: false,
	})

	var fullResponse strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 2. Stream chunks
	err := h.llm.GenerateChatCompletionStream(ctx, model, prompt, func(chunk string) error {
		fullResponse.WriteString(chunk)

		room.BroadcastMessage(&simplysocket.Message{
			Action: "llm-stream-chunk",
			Target: roomName,
			MessageBody: map[string]any{
				"stream_id": streamID,
				"chunk":     chunk,
				"sender":    senderSlug,
			},
			Sender:         "server",
			IsTargetClient: false,
		})
		return nil
	})

	// 3. Send stream end or error
	if err != nil {
		log.Error("LLM stream error", "stream_id", streamID, "error", err.Error())
		room.BroadcastMessage(&simplysocket.Message{
			Action: "llm-stream-error",
			Target: roomName,
			MessageBody: map[string]any{
				"stream_id": streamID,
				"error":     err.Error(),
			},
			Sender:         "server",
			IsTargetClient: false,
		})
		return
	}

	room.BroadcastMessage(&simplysocket.Message{
		Action: "llm-stream-end",
		Target: roomName,
		MessageBody: map[string]any{
			"stream_id": streamID,
			"full_text": fullResponse.String(),
			"sender":    senderSlug,
			"time":      time.Now().UTC().Format(time.RFC3339),
		},
		Sender:         "server",
		IsTargetClient: false,
	})
}

// buildMeshSnapshot collects real-time rooms, clients, and scheduled tasks from simplysocket.MeshServer and Scheduler.
func buildMeshSnapshot(server simplysocket.MeshServer, scheduler *jobs.Scheduler) map[string]any {
	clientsInRoomMap := server.GetClientsInRoom()
	allRooms := server.GetRooms()
	allClientsMap := server.GetClients()

	// Capture all rooms created and joined from GetClientsInRoom and GetRooms
	roomSet := make(map[string]bool)
	for rName := range clientsInRoomMap {
		roomSet[rName] = true
	}
	for _, rName := range allRooms {
		roomSet[rName] = true
	}
	// Default global room is always guaranteed
	roomSet[simplysocket.MeshGlobalRoom] = true

	roomsDetail := make(map[string]map[string]any)
	var finalRooms []string
	uniqueClients := make(map[string]bool)

	for rName := range roomSet {
		finalRooms = append(finalRooms, rName)
		clientMap := clientsInRoomMap[rName]
		var clientSlugs []string
		for slug := range clientMap {
			if slug != "" {
				clientSlugs = append(clientSlugs, slug)
				uniqueClients[slug] = true
			}
		}
		sort.Strings(clientSlugs)
		roomsDetail[rName] = map[string]any{
			"count":   len(clientSlugs),
			"clients": clientSlugs,
		}
	}

	for slug := range allClientsMap {
		if slug != "" {
			uniqueClients[slug] = true
		}
	}

	sort.Strings(finalRooms)

	var scheduledJobs []jobs.TaskInfo
	if scheduler != nil {
		scheduledJobs = scheduler.GetTasks()
	} else {
		scheduledJobs = make([]jobs.TaskInfo, 0)
	}

	return map[string]any{
		"rooms":          finalRooms,
		"total_rooms":    len(finalRooms),
		"total_clients":  len(uniqueClients),
		"rooms_detail":   roomsDetail,
		"scheduled_jobs": scheduledJobs,
		"server_name":    server.GetGameName(),
		"server_time":    time.Now().UTC().Format(time.RFC3339),
	}
}
