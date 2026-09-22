package webrtcserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hack-go-thon/config"
	"hack-go-thon/pkg/log"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// SessionDescription holds the SDP string and type ("offer" or "answer").
type SessionDescription struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

// SessionRequest is the payload sent by a client to initiate a server WebRTC session.
type SessionRequest struct {
	SDP string `json:"sdp"`
}

// SessionResponse is returned by the server containing the negotiated SDP answer.
type SessionResponse struct {
	SessionID string             `json:"session_id"`
	Answer    SessionDescription `json:"answer"`
}

// ServerPeerManager manages active server-side WebRTC peer connections.
type ServerPeerManager struct {
	config      *config.Config
	webrtcCfg   webrtc.Configuration
	connections map[string]*webrtc.PeerConnection
	mu          sync.RWMutex
}

// NewServerPeerManager initializes the Pion WebRTC manager with configured STUN/TURN servers.
func NewServerPeerManager(cfg *config.Config) *ServerPeerManager {
	iceServers := make([]webrtc.ICEServer, 0)

	// Add STUN servers
	if len(cfg.STUNServers) > 0 {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs: cfg.STUNServers,
		})
	}

	// Add TURN server if provided
	if cfg.TURNServerURL != "" {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:           []string{cfg.TURNServerURL},
			Username:       cfg.TURNUsername,
			Credential:     cfg.TURNCredential,
			CredentialType: webrtc.ICECredentialTypePassword,
		})
	}

	return &ServerPeerManager{
		config: cfg,
		webrtcCfg: webrtc.Configuration{
			ICEServers: iceServers,
		},
		connections: make(map[string]*webrtc.PeerConnection),
	}
}

// HandleOffer processes an incoming client SDP offer, configures an ultra-low latency DataChannel,
// waits for ICE gathering to complete, and returns the complete SDP answer.
func (m *ServerPeerManager) HandleOffer(ctx context.Context, offerSDP string) (*SessionResponse, error) {
	sessionID := uuid.New().String()

	// 1. Create PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(m.webrtcCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	// 2. Track connection in manager
	m.mu.Lock()
	m.connections[sessionID] = peerConnection
	m.mu.Unlock()

	// 3. Monitor connection state
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Info("WebRTC server peer connection state changed",
			"session_id", sessionID,
			"state", state.String(),
		)
		if state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateDisconnected {
			m.CloseSession(sessionID)
		}
	})

	// 4. Setup DataChannel listener for ultra-low latency UDP bidirectional messaging
	peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
		label := d.Label()
		log.Info("WebRTC DataChannel opened by client",
			"session_id", sessionID,
			"label", label,
		)

		d.OnOpen(func() {
			log.Info("DataChannel state is open", "session_id", sessionID, "label", label)
			_ = d.SendText(fmt.Sprintf("CONNECTED to hack-go-thon WebRTC server [session: %s]", sessionID))
		})

		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			text := string(msg.Data)
			log.Debug("DataChannel message received",
				"session_id", sessionID,
				"label", label,
				"bytes", len(msg.Data),
			)

			// Handle ping-pong latency benchmark
			if len(text) >= 5 && text[:5] == "ping:" {
				_ = d.SendText("pong:" + text[5:])
				return
			}

			// Echo back payload with timestamp
			echoReply := fmt.Sprintf(`{"echo": %q, "time": %q}`, text, time.Now().UTC().Format(time.RFC3339Nano))
			_ = d.SendText(echoReply)
		})
	})

	// 5. Parse and Set Remote Description (Offer)
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}
	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		_ = peerConnection.Close()
		m.CloseSession(sessionID)
		return nil, fmt.Errorf("failed to set remote description: %w", err)
	}

	// 6. Create Answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		_ = peerConnection.Close()
		m.CloseSession(sessionID)
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	// 7. Setup gathering complete promise so all local ICE candidates are bundled into the answer
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// 8. Set Local Description
	if err := peerConnection.SetLocalDescription(answer); err != nil {
		_ = peerConnection.Close()
		m.CloseSession(sessionID)
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	// 9. Wait for ICE candidate gathering to finish (or timeout)
	select {
	case <-gatherComplete:
	case <-time.After(3 * time.Second):
		log.Warn("WebRTC ICE gathering timed out, proceeding with gathered candidates", "session_id", sessionID)
	case <-ctx.Done():
		_ = peerConnection.Close()
		m.CloseSession(sessionID)
		return nil, ctx.Err()
	}

	localDesc := peerConnection.LocalDescription()
	if localDesc == nil {
		_ = peerConnection.Close()
		m.CloseSession(sessionID)
		return nil, fmt.Errorf("local description is nil after gathering")
	}

	return &SessionResponse{
		SessionID: sessionID,
		Answer: SessionDescription{
			Type: localDesc.Type.String(),
			SDP:  localDesc.SDP,
		},
	}, nil
}

// CloseSession closes and removes a session.
func (m *ServerPeerManager) CloseSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if pc, exists := m.connections[sessionID]; exists {
		_ = pc.Close()
		delete(m.connections, sessionID)
		log.Info("WebRTC server session closed", "session_id", sessionID)
	}
}

// ActiveSessionsCount returns the number of active WebRTC peer connections.
func (m *ServerPeerManager) ActiveSessionsCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.connections)
}
