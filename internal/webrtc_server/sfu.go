package webrtcserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"hack-go-thon/config"
	"hack-go-thon/pkg/log"

	"github.com/gorilla/websocket"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

var sfuUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// SFUTrack represents a published media track forwarded by the SFU.
type SFUTrack struct {
	ID        string
	Kind      string // "audio" or "video"
	Publisher string
	Track     *webrtc.TrackLocalStaticRTP
}

// SFUPeer represents a client connected to an SFU room.
type SFUPeer struct {
	ID                 string
	RoomID             string
	PC                 *webrtc.PeerConnection
	senders            map[string]*webrtc.RTPSender
	closeChan          chan struct{}
	wsConn             *websocket.Conn
	wsMu               sync.Mutex
	mu                 sync.RWMutex
	signaled           bool
	renegotiatePending bool
}

func (p *SFUPeer) writeJSON(v any) error {
	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	if p.wsConn != nil {
		return p.wsConn.WriteJSON(v)
	}
	return nil
}

// SFURoom coordinates multiple peers and forwards media tracks among them.
type SFURoom struct {
	ID     string
	peers  map[string]*SFUPeer
	tracks map[string]*SFUTrack
	mu     sync.RWMutex
}

// NewSFURoom creates a new SFU room.
func NewSFURoom(roomID string) *SFURoom {
	return &SFURoom{
		ID:     roomID,
		peers:  make(map[string]*SFUPeer),
		tracks: make(map[string]*SFUTrack),
	}
}

// signalPeerConnectionsLocked synchronizes all peers in this room so each peer receives all room tracks.
// Must be called with r.mu held.
func (r *SFURoom) signalPeerConnectionsLocked() {
	for _, peer := range r.peers {
		if peer.PC == nil || peer.PC.ConnectionState() == webrtc.PeerConnectionStateClosed {
			continue
		}

		hasChanges := false
		existingSenders := make(map[string]bool)

		// 1. Audit active senders: remove any track that no longer exists in r.tracks
		for _, sender := range peer.PC.GetSenders() {
			if sender.Track() == nil {
				continue
			}
			trackID := sender.Track().ID()
			if _, exists := r.tracks[trackID]; !exists {
				if err := peer.PC.RemoveTrack(sender); err == nil {
					hasChanges = true
					peer.mu.Lock()
					delete(peer.senders, trackID)
					peer.mu.Unlock()
				}
			} else {
				existingSenders[trackID] = true
			}
		}

		// 2. Add any room track that this peer does not yet send (excluding this peer's own published tracks)
		for trackID, sfuTrack := range r.tracks {
			if sfuTrack.Publisher == peer.ID {
				continue
			}
			if !existingSenders[trackID] {
				sender, err := peer.PC.AddTrack(sfuTrack.Track)
				if err == nil {
					hasChanges = true
					peer.mu.Lock()
					peer.senders[trackID] = sender
					peer.mu.Unlock()

					// Read RTCP from this sender so PLI/FIR requests from subscriber trigger keyframes
					go func(s *webrtc.RTPSender, pubID string) {
						for {
							pkts, _, rtcpErr := s.ReadRTCP()
							if rtcpErr != nil {
								return
							}
							for _, pkt := range pkts {
								switch pkt.(type) {
								case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
									r.mu.Lock()
									r.dispatchKeyFrameToPeerLocked(pubID)
									r.mu.Unlock()
								}
							}
						}
					}(sender, sfuTrack.Publisher)
				} else {
					log.Error("Failed to add track to SFU peer", "peer_id", peer.ID, "track_id", trackID, "error", err)
				}
			}
		}

		// 3. Negotiate if this peer is newly connecting or if tracks changed
		if peer.wsConn != nil {
			if !peer.signaled || hasChanges || peer.renegotiatePending {
				if peer.PC.SignalingState() != webrtc.SignalingStateStable {
					peer.renegotiatePending = true
					continue
				}

				offer, err := peer.PC.CreateOffer(nil)
				if err != nil {
					log.Error("Failed to create offer during SFU sync", "peer_id", peer.ID, "error", err)
					continue
				}

				if err := peer.PC.SetLocalDescription(offer); err != nil {
					log.Error("Failed to set local description during SFU sync", "peer_id", peer.ID, "error", err)
					continue
				}

				peer.signaled = true
				peer.renegotiatePending = false

				offerBytes, _ := json.Marshal(offer)
				_ = peer.writeJSON(map[string]any{
					"event": "offer",
					"data":  string(offerBytes),
				})
			}
		}
	}

	// Dispatch keyframes to all video receivers
	r.dispatchKeyFrameLocked()
}

// dispatchKeyFrameLocked sends RTCP PLI keyframe requests to all video receivers.
func (r *SFURoom) dispatchKeyFrameLocked() {
	for _, peer := range r.peers {
		if peer.PC == nil {
			continue
		}
		for _, receiver := range peer.PC.GetReceivers() {
			if receiver.Track() != nil && receiver.Track().Kind() == webrtc.RTPCodecTypeVideo {
				_ = peer.PC.WriteRTCP([]rtcp.Packet{
					&rtcp.PictureLossIndication{
						MediaSSRC: uint32(receiver.Track().SSRC()),
					},
				})
			}
		}
	}
}

// dispatchKeyFrameToPeerLocked sends an RTCP PLI request to a specific publisher peer.
func (r *SFURoom) dispatchKeyFrameToPeerLocked(peerID string) {
	pubPeer, exists := r.peers[peerID]
	if !exists || pubPeer.PC == nil {
		return
	}
	for _, receiver := range pubPeer.PC.GetReceivers() {
		if receiver.Track() != nil && receiver.Track().Kind() == webrtc.RTPCodecTypeVideo {
			_ = pubPeer.PC.WriteRTCP([]rtcp.Packet{
				&rtcp.PictureLossIndication{
					MediaSSRC: uint32(receiver.Track().SSRC()),
				},
			})
		}
	}
}

// SFUEngine coordinates all active SFU rooms and handles media packet forwarding.
type SFUEngine struct {
	cfg         *config.Config
	webrtcCfg   webrtc.Configuration
	rooms       map[string]*SFURoom
	onTrackHook func(roomID, publisherID, trackKind string)
	mu          sync.RWMutex
}

// NewSFUEngine initializes the Pion SFU Engine with configured STUN/TURN servers.
func NewSFUEngine(cfg *config.Config) *SFUEngine {
	iceServers := make([]webrtc.ICEServer, 0)
	if len(cfg.STUNServers) > 0 {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: cfg.STUNServers})
	}
	if cfg.TURNServerURL != "" {
		iceServers = append(iceServers, webrtc.ICEServer{
			URLs:           []string{cfg.TURNServerURL},
			Username:       cfg.TURNUsername,
			Credential:     cfg.TURNCredential,
			CredentialType: webrtc.ICECredentialTypePassword,
		})
	}

	engine := &SFUEngine{
		cfg: cfg,
		webrtcCfg: webrtc.Configuration{
			ICEServers: iceServers,
		},
		rooms: make(map[string]*SFURoom),
	}

	// Periodic keyframe dispatcher routine
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			engine.mu.RLock()
			for _, room := range engine.rooms {
				room.mu.Lock()
				room.dispatchKeyFrameLocked()
				room.mu.Unlock()
			}
			engine.mu.RUnlock()
		}
	}()

	return engine
}

// SetOnTrackHook attaches an optional callback invoked when a new media track is published.
func (e *SFUEngine) SetOnTrackHook(hook func(roomID, publisherID, trackKind string)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onTrackHook = hook
}

// GetOrCreateRoom returns an existing room or instantiates a new one.
func (e *SFUEngine) GetOrCreateRoom(roomID string) *SFURoom {
	e.mu.Lock()
	defer e.mu.Unlock()

	r, exists := e.rooms[roomID]
	if !exists {
		r = NewSFURoom(roomID)
		e.rooms[roomID] = r
		log.Info("SFU room created", "room_id", roomID)
	}
	return r
}

// HandleWebSocket manages the live, bidirectional WebRTC signaling for an SFU conference room.
func (e *SFUEngine) HandleWebSocket(w http.ResponseWriter, r *http.Request, roomID, peerID string) {
	conn, err := sfuUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Failed to upgrade SFU WebSocket", "error", err)
		return
	}
	defer conn.Close()

	room := e.GetOrCreateRoom(roomID)

	pc, err := webrtc.NewPeerConnection(e.webrtcCfg)
	if err != nil {
		log.Error("Failed to create SFU PeerConnection", "error", err)
		return
	}
	defer pc.Close()

	peer := &SFUPeer{
		ID:        peerID,
		RoomID:    roomID,
		PC:        pc,
		senders:   make(map[string]*webrtc.RTPSender),
		closeChan: make(chan struct{}),
		wsConn:    conn,
	}

	// 1. Add incoming transceivers so the server can receive camera and microphone streams
	for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
		if _, err := pc.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		}); err != nil {
			log.Error("Failed to add transceiver", "error", err)
			return
		}
	}

	// 2. Handle remote media tracks published by this client
	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		kind := remoteTrack.Kind().String()
		trackID := fmt.Sprintf("%s_%s", peerID, kind)

		log.Info("SFU WebSocket received published track",
			"room_id", roomID,
			"peer_id", peerID,
			"kind", kind,
			"track_id", trackID,
		)

		localTrack, trackErr := webrtc.NewTrackLocalStaticRTP(
			remoteTrack.Codec().RTPCodecCapability,
			trackID,
			peerID,
		)
		if trackErr != nil {
			log.Error("Failed to create local fan-out track", "error", trackErr)
			return
		}

		sfuTrack := &SFUTrack{
			ID:        trackID,
			Kind:      kind,
			Publisher: peerID,
			Track:     localTrack,
		}

		room.mu.Lock()
		room.tracks[trackID] = sfuTrack
		// Signal all connected peers in this room so they receive this track immediately
		room.signalPeerConnectionsLocked()
		room.mu.Unlock()

		// Trigger hook if attached
		e.mu.RLock()
		hook := e.onTrackHook
		e.mu.RUnlock()
		if hook != nil {
			hook(roomID, peerID, kind)
		}

		// Forward raw RTP packets with header extension stripping for cross-browser decoding
		buf := make([]byte, 1500)
		rtpPkt := &rtp.Packet{}
		for {
			i, _, readErr := remoteTrack.Read(buf)
			if readErr != nil {
				return
			}
			if unmarshalErr := rtpPkt.Unmarshal(buf[:i]); unmarshalErr != nil {
				continue
			}
			rtpPkt.Extension = false
			rtpPkt.Extensions = nil
			if writeErr := localTrack.WriteRTP(rtpPkt); writeErr != nil {
				// Don't terminate forwarding loop on transient write errors
				continue
			}
		}
	})

	// 3. Trickle ICE: forward server ICE candidates to client
	pc.OnICECandidate(func(i *webrtc.ICECandidate) {
		if i == nil {
			return
		}
		candJSON, err := json.Marshal(i.ToJSON())
		if err == nil {
			_ = peer.writeJSON(map[string]any{
				"event": "candidate",
				"data":  string(candJSON),
			})
		}
	})

	// 4. Lifecycle state monitor
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Info("SFU WebSocket peer connection state changed",
			"room_id", roomID,
			"peer_id", peerID,
			"state", state.String(),
		)
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			e.LeaveRoom(roomID, peerID)
		}
	})

	// 5. Register peer in room and initiate synchronization
	room.mu.Lock()
	room.peers[peerID] = peer
	room.signalPeerConnectionsLocked()
	room.mu.Unlock()

	// 6. Handle incoming client messages (answer, candidate)
	for {
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		var msg struct {
			Event string `json:"event"`
			Data  string `json:"data"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		switch msg.Event {
		case "answer":
			var answer webrtc.SessionDescription
			if err := json.Unmarshal([]byte(msg.Data), &answer); err == nil {
				if err := pc.SetRemoteDescription(answer); err != nil {
					log.Error("Failed to set remote description for SFU answer", "peer_id", peerID, "error", err)
				} else {
					room.mu.Lock()
					if peer.renegotiatePending {
						room.signalPeerConnectionsLocked()
					}
					room.dispatchKeyFrameLocked()
					room.mu.Unlock()
				}
			}
		case "candidate":
			var cand webrtc.ICECandidateInit
			if err := json.Unmarshal([]byte(msg.Data), &cand); err == nil {
				_ = pc.AddICECandidate(cand)
			}
		}
	}

	// 7. Cleanup upon disconnect
	e.LeaveRoom(roomID, peerID)
}

// SFUJoinRequest is the payload from a client wishing to publish/subscribe via REST.
type SFUJoinRequest struct {
	RoomID string `json:"room_id" binding:"required"`
	PeerID string `json:"peer_id" binding:"required"`
	SDP    string `json:"sdp" binding:"required"`
}

// SFUJoinResponse is returned to the client containing the negotiated SDP answer.
type SFUJoinResponse struct {
	Status       string             `json:"status"`
	RoomID       string             `json:"room_id"`
	PeerID       string             `json:"peer_id"`
	Answer       SessionDescription `json:"answer"`
	ActivePeers  int                `json:"active_peers"`
	ActiveTracks int                `json:"active_tracks"`
}

// JoinRoom negotiates a client connection via REST SDP offer/answer.
func (e *SFUEngine) JoinRoom(ctx context.Context, req SFUJoinRequest) (*SFUJoinResponse, error) {
	room := e.GetOrCreateRoom(req.RoomID)

	pc, err := webrtc.NewPeerConnection(e.webrtcCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	peer := &SFUPeer{
		ID:        req.PeerID,
		RoomID:    req.RoomID,
		PC:        pc,
		senders:   make(map[string]*webrtc.RTPSender),
		closeChan: make(chan struct{}),
	}

	// Subscribe this peer to all existing tracks
	room.mu.RLock()
	for _, sfuTrack := range room.tracks {
		if sfuTrack.Publisher != req.PeerID {
			sender, addErr := pc.AddTrack(sfuTrack.Track)
			if addErr == nil {
				peer.senders[sfuTrack.ID] = sender
			}
		}
	}
	room.mu.RUnlock()

	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		kind := remoteTrack.Kind().String()
		trackID := fmt.Sprintf("%s_%s", req.PeerID, kind)

		localTrack, trackErr := webrtc.NewTrackLocalStaticRTP(
			remoteTrack.Codec().RTPCodecCapability,
			trackID,
			req.PeerID,
		)
		if trackErr != nil {
			log.Error("Failed to create local track", "error", trackErr)
			return
		}

		sfuTrack := &SFUTrack{
			ID:        trackID,
			Kind:      kind,
			Publisher: req.PeerID,
			Track:     localTrack,
		}

		room.mu.Lock()
		room.tracks[trackID] = sfuTrack
		room.signalPeerConnectionsLocked()
		room.mu.Unlock()

		e.mu.RLock()
		hook := e.onTrackHook
		e.mu.RUnlock()
		if hook != nil {
			hook(req.RoomID, req.PeerID, kind)
		}

		buf := make([]byte, 1500)
		rtpPkt := &rtp.Packet{}
		for {
			i, _, readErr := remoteTrack.Read(buf)
			if readErr != nil {
				return
			}
			if unmarshalErr := rtpPkt.Unmarshal(buf[:i]); unmarshalErr != nil {
				continue
			}
			rtpPkt.Extension = false
			rtpPkt.Extensions = nil
			if writeErr := localTrack.WriteRTP(rtpPkt); writeErr != nil {
				// Don't terminate forwarding loop on transient write errors
				continue
			}
		}
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateDisconnected {
			e.LeaveRoom(req.RoomID, req.PeerID)
		}
	})

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  req.SDP,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("failed to set remote description: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(pc)

	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	select {
	case <-gatherComplete:
	case <-time.After(3 * time.Second):
		log.Warn("SFU candidate gathering timeout, proceeding", "peer_id", req.PeerID)
	case <-ctx.Done():
		_ = pc.Close()
		return nil, ctx.Err()
	}

	localDesc := pc.LocalDescription()
	if localDesc == nil {
		_ = pc.Close()
		return nil, fmt.Errorf("local description is nil after gathering")
	}

	room.mu.Lock()
	room.peers[req.PeerID] = peer
	activePeersCount := len(room.peers)
	activeTracksCount := len(room.tracks)
	room.mu.Unlock()

	return &SFUJoinResponse{
		Status: "connected",
		RoomID: req.RoomID,
		PeerID: req.PeerID,
		Answer: SessionDescription{
			Type: localDesc.Type.String(),
			SDP:  localDesc.SDP,
		},
		ActivePeers:  activePeersCount,
		ActiveTracks: activeTracksCount,
	}, nil
}

// Renegotiate handles SDP offer from an existing peer.
func (e *SFUEngine) Renegotiate(ctx context.Context, roomID, peerID, offerSDP string) (*SessionDescription, error) {
	room := e.GetOrCreateRoom(roomID)

	room.mu.RLock()
	peer, exists := room.peers[peerID]
	room.mu.RUnlock()

	if !exists || peer.PC == nil {
		return nil, fmt.Errorf("peer %q not found in room %q", peerID, roomID)
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}

	if err := peer.PC.SetRemoteDescription(offer); err != nil {
		return nil, fmt.Errorf("failed to set remote description: %w", err)
	}

	answer, err := peer.PC.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(peer.PC)
	if err := peer.PC.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	select {
	case <-gatherComplete:
	case <-time.After(2 * time.Second):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	localDesc := peer.PC.LocalDescription()
	if localDesc == nil {
		return nil, fmt.Errorf("local description is nil")
	}

	return &SessionDescription{
		Type: localDesc.Type.String(),
		SDP:  localDesc.SDP,
	}, nil
}

// LeaveRoom removes a peer from an SFU room and cleans up resources.
func (e *SFUEngine) LeaveRoom(roomID, peerID string) {
	e.mu.Lock()
	room, exists := e.rooms[roomID]
	e.mu.Unlock()
	if !exists {
		return
	}

	room.mu.Lock()
	peer, peerExists := room.peers[peerID]
	if peerExists {
		close(peer.closeChan)
		if peer.wsConn != nil {
			_ = peer.wsConn.Close()
		}
		_ = peer.PC.Close()
		delete(room.peers, peerID)
		log.Info("SFU peer left room", "room_id", roomID, "peer_id", peerID)
	}

	// Remove tracks published by this peer
	for tID, t := range room.tracks {
		if t.Publisher == peerID {
			delete(room.tracks, tID)
		}
	}

	empty := len(room.peers) == 0
	if !empty {
		room.signalPeerConnectionsLocked()
	}
	room.mu.Unlock()

	if empty {
		e.mu.Lock()
		delete(e.rooms, roomID)
		e.mu.Unlock()
		log.Info("SFU room closed (empty)", "room_id", roomID)
	}
}

// SFURoomSummary provides status metrics for an SFU room.
type SFURoomSummary struct {
	ID         string   `json:"id"`
	PeerCount  int      `json:"peer_count"`
	Peers      []string `json:"peers"`
	TrackCount int      `json:"track_count"`
}

// GetRoomsSummary returns a snapshot of all active SFU rooms.
func (e *SFUEngine) GetRoomsSummary() []SFURoomSummary {
	e.mu.RLock()
	defer e.mu.RUnlock()

	summaries := make([]SFURoomSummary, 0, len(e.rooms))
	for rID, r := range e.rooms {
		r.mu.RLock()
		peersList := make([]string, 0, len(r.peers))
		for pID := range r.peers {
			peersList = append(peersList, pID)
		}
		summaries = append(summaries, SFURoomSummary{
			ID:         rID,
			PeerCount:  len(r.peers),
			Peers:      peersList,
			TrackCount: len(r.tracks),
		})
		r.mu.RUnlock()
	}
	return summaries
}
