package handler

import (
	"hack-go-thon/config"
	webrtcserver "hack-go-thon/internal/webrtc_server"
	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
)

// ICEServerConfig represents standard WebRTC RTCIceServer dictionary.
type ICEServerConfig struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// WebRTCHandler provides HTTP endpoints for WebRTC configuration, server sessions, and SFU.
type WebRTCHandler struct {
	config        *config.Config
	serverManager *webrtcserver.ServerPeerManager
	sfuEngine     *webrtcserver.SFUEngine
}

// NewWebRTCHandler creates a new WebRTCHandler.
func NewWebRTCHandler(cfg *config.Config, manager *webrtcserver.ServerPeerManager, sfu *webrtcserver.SFUEngine) *WebRTCHandler {
	return &WebRTCHandler{
		config:        cfg,
		serverManager: manager,
		sfuEngine:     sfu,
	}
}

// GetICEServers godoc
// @Summary Get WebRTC ICE servers configuration
// @Description Returns STUN and TURN server credentials formatted for RTCPeerConnection.
// @Tags webrtc
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/webrtc/ice-servers [get]
func (h *WebRTCHandler) GetICEServers(c *gin.Context) {
	iceServers := make([]ICEServerConfig, 0)

	// Add STUN servers
	if len(h.config.STUNServers) > 0 {
		iceServers = append(iceServers, ICEServerConfig{
			URLs: h.config.STUNServers,
		})
	}

	// Add TURN server if configured
	if h.config.TURNServerURL != "" {
		iceServers = append(iceServers, ICEServerConfig{
			URLs:       []string{h.config.TURNServerURL},
			Username:   h.config.TURNUsername,
			Credential: h.config.TURNCredential,
		})
	}

	response.OK(c, gin.H{
		"ice_servers": iceServers,
	})
}

// ServerSessionRequest defines payload to negotiate server-side WebRTC session.
type ServerSessionRequest struct {
	SDP string `json:"sdp" binding:"required"`
}

// CreateServerSession godoc
// @Summary Create a server-side WebRTC session
// @Description Accepts client SDP offer, creates server peer connection, and returns SDP answer.
// @Tags webrtc
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/v1/webrtc/server/session [post]
func (h *WebRTCHandler) CreateServerSession(c *gin.Context) {
	var req ServerSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid request: 'sdp' is required"))
		return
	}

	if h.serverManager == nil {
		response.Error(c, apperrors.NewInternal("WebRTC server peer manager is not configured"))
		return
	}

	sessionResp, err := h.serverManager.HandleOffer(c.Request.Context(), req.SDP)
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to negotiate WebRTC session: "+err.Error()))
		return
	}

	response.OK(c, sessionResp)
}

// CloseServerSession godoc
// @Summary Close an active server WebRTC session
// @Tags webrtc
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/webrtc/server/session/:id [delete]
func (h *WebRTCHandler) CloseServerSession(c *gin.Context) {
	sessionID := c.Param("id")
	if sessionID == "" {
		response.Error(c, apperrors.NewBadRequest("Session ID is required"))
		return
	}

	if h.serverManager != nil {
		h.serverManager.CloseSession(sessionID)
	}

	response.OK(c, gin.H{
		"status":     "closed",
		"session_id": sessionID,
	})
}

// GetStatus godoc
// @Summary WebRTC subsystem status
// @Tags webrtc
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/webrtc/status [get]
func (h *WebRTCHandler) GetStatus(c *gin.Context) {
	activeSessions := 0
	if h.serverManager != nil {
		activeSessions = h.serverManager.ActiveSessionsCount()
	}

	sfuRooms := 0
	sfuPeers := 0
	if h.sfuEngine != nil {
		summaries := h.sfuEngine.GetRoomsSummary()
		sfuRooms = len(summaries)
		for _, s := range summaries {
			sfuPeers += s.PeerCount
		}
	}

	response.OK(c, gin.H{
		"active_server_sessions": activeSessions,
		"sfu_active_rooms":       sfuRooms,
		"sfu_active_peers":       sfuPeers,
		"stun_servers":           h.config.STUNServers,
		"turn_configured":        h.config.TURNServerURL != "",
	})
}

// JoinSFU godoc
// @Summary Join an SFU conference room
// @Description Ingests publisher SDP offer, sets up fan-out RTP tracks, and returns SDP answer.
// @Tags webrtc-sfu
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/v1/webrtc/sfu/join [post]
func (h *WebRTCHandler) JoinSFU(c *gin.Context) {
	if h.sfuEngine == nil {
		response.Error(c, apperrors.NewInternal("SFU engine is not configured"))
		return
	}

	var req webrtcserver.SFUJoinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid request: 'room_id', 'peer_id', and 'sdp' are required"))
		return
	}

	resp, err := h.sfuEngine.JoinRoom(c.Request.Context(), req)
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to join SFU room: "+err.Error()))
		return
	}

	response.OK(c, resp)
}

// SFURenegotiateRequest represents a renegotiation offer when new tracks join an SFU room.
type SFURenegotiateRequest struct {
	RoomID string `json:"room_id" binding:"required"`
	PeerID string `json:"peer_id" binding:"required"`
	SDP    string `json:"sdp" binding:"required"`
}

// RenegotiateSFU godoc
// @Summary Renegotiate an existing SFU peer connection
// @Description Updates local description with new room tracks and returns updated SDP answer.
// @Tags webrtc-sfu
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 500 {object} response.Response
// @Router /api/v1/webrtc/sfu/renegotiate [post]
func (h *WebRTCHandler) RenegotiateSFU(c *gin.Context) {
	if h.sfuEngine == nil {
		response.Error(c, apperrors.NewInternal("SFU engine is not configured"))
		return
	}

	var req SFURenegotiateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid request: 'room_id', 'peer_id', and 'sdp' are required"))
		return
	}

	answer, err := h.sfuEngine.Renegotiate(c.Request.Context(), req.RoomID, req.PeerID, req.SDP)
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to renegotiate SFU session: "+err.Error()))
		return
	}

	response.OK(c, gin.H{
		"room_id": req.RoomID,
		"peer_id": req.PeerID,
		"answer":  answer,
	})
}

// SFULeaveRequest represents a client leaving an SFU room.
type SFULeaveRequest struct {
	RoomID string `json:"room_id"`
	PeerID string `json:"peer_id"`
}

// LeaveSFU godoc
// @Summary Leave an SFU conference room
// @Description Closes peer connection and frees forwarded RTP tracks.
// @Tags webrtc-sfu
// @Accept json
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/webrtc/sfu/leave [post]
func (h *WebRTCHandler) LeaveSFU(c *gin.Context) {
	if h.sfuEngine == nil {
		response.Error(c, apperrors.NewInternal("SFU engine is not configured"))
		return
	}

	roomID := c.Param("room_id")
	peerID := c.Param("peer_id")

	if roomID == "" || peerID == "" {
		var req SFULeaveRequest
		if err := c.ShouldBindJSON(&req); err == nil {
			if req.RoomID != "" {
				roomID = req.RoomID
			}
			if req.PeerID != "" {
				peerID = req.PeerID
			}
		}
	}

	if roomID == "" || peerID == "" {
		response.Error(c, apperrors.NewBadRequest("room_id and peer_id are required"))
		return
	}

	h.sfuEngine.LeaveRoom(roomID, peerID)

	response.OK(c, gin.H{
		"status":  "left",
		"room_id": roomID,
		"peer_id": peerID,
	})
}

// GetSFURooms godoc
// @Summary List active SFU conference rooms
// @Description Returns list of active rooms with participant IDs and track counts.
// @Tags webrtc-sfu
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/webrtc/sfu/rooms [get]
func (h *WebRTCHandler) GetSFURooms(c *gin.Context) {
	if h.sfuEngine == nil {
		response.OK(c, []any{})
		return
	}

	summaries := h.sfuEngine.GetRoomsSummary()
	response.OK(c, summaries)
}

// HandleSFUWS upgrades a client HTTP connection to WebSocket for live bidirectional SFU media negotiation.
func (h *WebRTCHandler) HandleSFUWS(c *gin.Context) {
	if h.sfuEngine == nil {
		response.Error(c, apperrors.NewInternal("SFU engine is not configured"))
		return
	}
	roomID := c.Query("room")
	if roomID == "" {
		roomID = "sfu-conf"
	}
	peerID := c.Query("peer")
	if peerID == "" {
		peerID = "peer_" + c.ClientIP()
	}

	h.sfuEngine.HandleWebSocket(c.Writer, c.Request, roomID, peerID)
}
