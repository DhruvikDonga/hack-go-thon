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

// WebRTCHandler provides HTTP endpoints for WebRTC configuration and server-side sessions.
type WebRTCHandler struct {
	config        *config.Config
	serverManager *webrtcserver.ServerPeerManager
}

// NewWebRTCHandler creates a new WebRTCHandler.
func NewWebRTCHandler(cfg *config.Config, manager *webrtcserver.ServerPeerManager) *WebRTCHandler {
	return &WebRTCHandler{
		config:        cfg,
		serverManager: manager,
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

	response.OK(c, gin.H{
		"active_server_sessions": activeSessions,
		"stun_servers":           h.config.STUNServers,
		"turn_configured":        h.config.TURNServerURL != "",
	})
}
