package ws

import (
	"sync"
	"time"

	"hack-go-thon/pkg/log"

	"github.com/DhruvikDonga/simplysocket"
)

// WebRTCPeer represents a connected peer participating in a WebRTC signaling session.
type WebRTCPeer struct {
	ID       string    `json:"id"`
	JoinedAt time.Time `json:"joined_at"`
}

// WebRTCRoomHandler coordinates SDP offers, SDP answers, and ICE candidate exchanges
// for real-time peer-to-peer audio, video, and data channels over simplysocket.
type WebRTCRoomHandler struct {
	slug  string
	peers map[string]*WebRTCPeer
	mu    sync.RWMutex
}

// NewWebRTCRoomHandler creates a new WebRTC signaling handler for a specific room slug.
func NewWebRTCRoomHandler(slug string) *WebRTCRoomHandler {
	if slug == "" {
		slug = "webrtc"
	}
	return &WebRTCRoomHandler{
		slug:  slug,
		peers: make(map[string]*WebRTCPeer),
	}
}

// Slug returns the room slug identifier.
func (h *WebRTCRoomHandler) Slug() string {
	return h.slug
}

// GetPeers returns a snapshot of active peers in this signaling room.
func (h *WebRTCRoomHandler) GetPeers() []*WebRTCPeer {
	h.mu.RLock()
	defer h.mu.RUnlock()

	peers := make([]*WebRTCPeer, 0, len(h.peers))
	for _, p := range h.peers {
		peers = append(peers, &WebRTCPeer{
			ID:       p.ID,
			JoinedAt: p.JoinedAt,
		})
	}
	return peers
}

// HandleRoomData implements simplysocket.RoomData.
func (h *WebRTCRoomHandler) HandleRoomData(room simplysocket.Room, server simplysocket.MeshServer) {
	roomName := room.GetRoomSlugInfo()
	log.Info("WebRTCRoomHandler initialized for room", "room", roomName)

	for {
		select {
		case msg, ok := <-room.ConsumeRoomMessage():
			if !ok {
				log.Info("WebRTC room message channel closed", "room", roomName)
				return
			}

			sender := msg.Sender
			if sender == "" {
				sender = "anonymous"
			}

			log.Debug("WebRTC signaling message received",
				"action", msg.Action,
				"sender", sender,
				"room", roomName,
			)

			switch msg.Action {
			case "webrtc-join":
				h.mu.Lock()
				h.peers[sender] = &WebRTCPeer{
					ID:       sender,
					JoinedAt: time.Now().UTC(),
				}
				peerList := make([]string, 0, len(h.peers))
				for id := range h.peers {
					if id != sender {
						peerList = append(peerList, id)
					}
				}
				h.mu.Unlock()

				log.Info("Peer joined WebRTC room", "peer_id", sender, "room", roomName)

				// 1. Send existing peer list back to the newly joined peer
				room.BroadcastMessage(&simplysocket.Message{
					Action: "webrtc-peers",
					Target: roomName,
					MessageBody: map[string]any{
						"status":   "success",
						"room":     roomName,
						"your_id":  sender,
						"peers":    peerList,
						"peer_cnt": len(peerList),
					},
					Sender:         "server",
					IsTargetClient: false,
				})

				// 2. Announce new peer to other participants in the room
				room.BroadcastMessage(&simplysocket.Message{
					Action: "webrtc-peer-joined",
					Target: roomName,
					MessageBody: map[string]any{
						"peer_id": sender,
						"room":    roomName,
						"time":    time.Now().UTC().Format(time.RFC3339),
					},
					Sender:         "server",
					IsTargetClient: false,
				})

			case "webrtc-offer":
				// Forward SDP Offer to target peer
				targetID, _ := msg.MessageBody["target_id"].(string)
				sdp := msg.MessageBody["sdp"]

				room.BroadcastMessage(&simplysocket.Message{
					Action: "webrtc-offer",
					Target: roomName,
					MessageBody: map[string]any{
						"sender_id": sender,
						"target_id": targetID,
						"sdp":       sdp,
					},
					Sender:         sender,
					IsTargetClient: false,
				})

			case "webrtc-answer":
				// Forward SDP Answer back to initiating peer
				targetID, _ := msg.MessageBody["target_id"].(string)
				sdp := msg.MessageBody["sdp"]

				room.BroadcastMessage(&simplysocket.Message{
					Action: "webrtc-answer",
					Target: roomName,
					MessageBody: map[string]any{
						"sender_id": sender,
						"target_id": targetID,
						"sdp":       sdp,
					},
					Sender:         sender,
					IsTargetClient: false,
				})

			case "webrtc-ice":
				// Forward ICE candidate to target peer
				targetID, _ := msg.MessageBody["target_id"].(string)
				candidate := msg.MessageBody["candidate"]

				room.BroadcastMessage(&simplysocket.Message{
					Action: "webrtc-ice",
					Target: roomName,
					MessageBody: map[string]any{
						"sender_id": sender,
						"target_id": targetID,
						"candidate": candidate,
					},
					Sender:         sender,
					IsTargetClient: false,
				})

			case "webrtc-leave":
				h.mu.Lock()
				delete(h.peers, sender)
				h.mu.Unlock()

				log.Info("Peer left WebRTC room", "peer_id", sender, "room", roomName)

				room.BroadcastMessage(&simplysocket.Message{
					Action: "webrtc-peer-left",
					Target: roomName,
					MessageBody: map[string]any{
						"peer_id": sender,
						"room":    roomName,
					},
					Sender:         "server",
					IsTargetClient: false,
				})
			}

		case clientEvent := <-room.EventTriggers():
			if len(clientEvent) >= 3 {
				eventType := clientEvent[0]
				clientID := clientEvent[2]

				if eventType == "client_disconnected" || eventType == "client_left" {
					h.mu.Lock()
					_, exists := h.peers[clientID]
					if exists {
						delete(h.peers, clientID)
					}
					h.mu.Unlock()

					if exists {
						log.Info("WebRTC peer disconnected via event trigger", "peer_id", clientID)
						room.BroadcastMessage(&simplysocket.Message{
							Action: "webrtc-peer-left",
							Target: roomName,
							MessageBody: map[string]any{
								"peer_id": clientID,
								"room":    roomName,
							},
							Sender:         "server",
							IsTargetClient: false,
						})
					}
				}
			}
		}
	}
}
