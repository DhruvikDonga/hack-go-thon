---
name: hack-go-thon-dev
description: >-
  Comprehensive agent development skill and playbook for hack-go-thon.
  Use when developing, extending, or debugging features including REST endpoints,
  simplysocket WebSocket messaging, pgvector RAG, background cron jobs, and admin dashboard observability.
---

# `hack-go-thon` Agent Development Skill & Playbook

This document is the authoritative guide for AI coding agents and developers working on the `hack-go-thon` codebase. It outlines the architectural patterns, invariants, safety rules, and step-by-step implementation recipes required to build, test, and ship features rapidly without breaking production stability.

---

## 1. System Architecture & Tech Stack

`hack-go-thon` is a high-performance Go backend built for hackathons and production MVPs:

* **Language**: Go 1.22+
* **HTTP Router**: `gin-gonic/gin` with structured middleware chains (CORS, RequestID, Zap logger, Recovery).
* **Database**: PostgreSQL 16 + `pgvector` extension (`vector(1536)` embeddings).
* **Database Access**: Standard `database/sql` with `github.com/lib/pq`.
* **Real-time Engine**: [`simplysocket`](https://github.com/DhruvikDonga/simplysocket) mesh WebSocket server supporting multi-room multiplexing.
* **AI & RAG Engine**: Native vector store with HNSW cosine distance indexing (`<=>`), LLM token streaming, and zero-key offline mock fallback.
* **WebRTC Real-Time Media**: [`simplysocket`](https://github.com/DhruvikDonga/simplysocket) P2P signaling hub + [`pion/webrtc/v4`](https://github.com/pion/webrtc) server peer manager with UDP `RTCDataChannel` support.
* **Background Scheduler**: **Custom in-process scheduler** (`internal/jobs/scheduler.go`) with goroutine panic isolation (`runtime/debug.Stack()`), graceful cancellation, and live observability telemetry (`TaskInfo`). **Zero external cron dependencies.**
* **Logging**: `uber-go/zap` structured logging.
* **Admin Dashboard**: Zero-dependency single-page UI embedded directly via Go `embed.FS` at `/admin` and `/`.

### Directory Layout

```text
hack-go-thon/
├── cmd/server/main.go            # Application entrypoint & conditional dependency injection
├── config/
│   ├── config.go                 # Environment variable loading & defaults (w/ STUN/TURN)
│   └── services.go               # Subsystem feature flags loader (services.json)
├── services.json                 # Optional JSON config to selectively toggle subsystems
├── internal/
│   ├── api/
│   │   ├── router.go             # Gin HTTP router & route mounts (nil-safe service checks)
│   │   └── handler/              # Gin HTTP handlers (health, example, rag, jobs, webrtc, users)
│   ├── db_client/
│   │   └── postgres.go           # database/sql Postgres connection pool
│   ├── store/
│   │   ├── store.go              # Storage interfaces (DocumentStore, etc.)
│   │   └── pg_store/             # PostgreSQL + pgvector + users implementations
│   ├── ws/
│   │   ├── manager.go            # simplysocket Manager wrapper & broadcast safety
│   │   ├── handler.go            # WebSocket connection upgrade & client registration
│   │   ├── admin_handler.go      # Admin room handler & LLM token streaming
│   │   └── webrtc_handler.go     # WebRTC P2P signaling room handler
│   ├── webrtc_server/
│   │   ├── server.go             # Pion WebRTC server peer manager & UDP DataChannel
│   │   └── sfu.go                # SFU Engine with multi-party track router & PLI heartbeats
│   ├── llm_client/
│   │   └── client.go             # LLM completions, streaming & embeddings (w/ offline fallback)
│   ├── jobs/
│   │   ├── job.go                # Job interface and funcJob adapter
│   │   └── scheduler.go          # Custom in-process scheduler with TaskInfo telemetry
│   └── worker/
│       └── worker.go             # Background loop worker runner
├── pkg/                          # Shared reusable packages (logger, apperrors, response)
└── web/
    ├── web.go                    # Go embed.FS declaration
    └── admin.html                # Embedded dark-mode admin control center (w/ SFU Video Lab)
```

---

## 2. Core Invariants & Safety Rules (DO NOT BREAK)

When generating or refactoring code in this repository, strictly adhere to these rules:

### A. `simplysocket` Room Lifecycle & Nil Safety

1. **Dynamic Room Lifecycle**: In `simplysocket`, rooms other than `MeshGlobalRoom` are dynamically created when clients join and deleted when all clients leave.
2. **Never register synthetic clients**: Calling `server.JoinClientRoom(room, "system")` will insert `nil` into `clientsinroom[room]["system"]`, causing a `panic: runtime error: invalid memory address or nil pointer dereference` when broadcasting. Only join actual connected client IDs (`msg.Sender`).
3. **Guard Broadcasts against empty rooms**: Sending messages to non-existent or empty rooms panics inside `meshServer.RunMeshServer`. Always check if `targetRoom == ws.MeshGlobalRoom` or if `targetRoom` exists in `server.GetRooms()` before calling `server.PushMessage`.
4. **Always use `wsManager.Broadcast`**: Do not invoke `server.PushMessage` directly from handlers; use `wsManager.Broadcast(ctx, targetRoom, action, data)` which has built-in safety guards.

### B. PostgreSQL & pgvector Rules
1. **Embedding Dimension**: Default vector size is `1536` (matching OpenAI / Gemini embedding models).
2. **Vector Serialization**:
   * Always write vectors to SQL using `pg_store.FormatVector(embedding)` -> `"[0.0123,0.456,...]"`.
   * Always parse vectors from SQL using `pg_store.ParseVector(vectorStr)`.
3. **Distance Operator**: Use `<=>` for cosine distance queries:
   ```sql
   SELECT id, title, content, 1 - (embedding <=> $1::vector) AS similarity
   FROM documents
   ORDER BY embedding <=> $1::vector
   LIMIT $2;
   ```
4. **Index Type**: Index must be HNSW with cosine distance:
   ```sql
   CREATE INDEX IF NOT EXISTS documents_embedding_hnsw_idx 
   ON documents USING hnsw (embedding vector_cosine_ops);
   ```

### C. LLM Streaming & Zero-Key Demo Mode

1. Always maintain the offline simulated fallback in `internal/llm_client`. If `API_KEY` is empty, generate realistic simulated stream chunks and deterministic unit-norm mock embeddings. **Tests and hackathon demos must never crash due to a missing API key.**
2. Wire protocol for LLM streaming over WebSockets:
   * Client sends: `{"action": "llm-stream-request", "room": "...", "data": "prompt"}`
   * Server emits: `{"action": "llm-stream-chunk", "room": "...", "data": "token"}` (streamed)
   * Server emits: `{"action": "llm-stream-end", "room": "...", "data": ""}` (finished)

### D. Concurrency & Goroutine Safety
1. Always test code with the race detector: `go test -v -race ./...`.
2. Protect shared mutable state with `sync.RWMutex` (see `internal/jobs/scheduler.go` for reference).

---

## 3. Step-by-Step Recipes for Agents

### Recipe 1: Adding a New REST Endpoint

1. **Define the Data Model & Interface**:

   In `internal/store/`, declare the model struct and interface:
   ```go
   type ItemModel struct {
       ID        string    `json:"id"`
       Title     string    `json:"title"`
       CreatedAt time.Time `json:"created_at"`
   }
   type ItemStore interface {
       CreateItem(ctx context.Context, item *ItemModel) error
       GetItem(ctx context.Context, id string) (*ItemModel, error)
   }
   ```
2. **Implement in PostgreSQL Store**:

   In `internal/store/pg_store/`, implement using `db.Client.ExecContext` and `db.Client.QueryRowContext`.
3. **Create the HTTP Handler (Gin)**:

   In `internal/api/handler/item_handler.go`:
   ```go
   type ItemHandler struct {
       store ItemStore
   }
   func (h *ItemHandler) Create(c *gin.Context) {
       var req CreateItemRequest
       if err := c.ShouldBindJSON(&req); err != nil {
           response.BadRequest(c, err.Error())
           return
       }
       // Process and respond
       response.Created(c, "Item created successfully", result)
   }
   ```
4. **Mount in Gin Router**:
   In `internal/api/router.go`, register route under the API group:
   ```go
   items := apiV1.Group("/items")
   {
       items.POST("", itemHandler.Create)
   }
   ```
5. **Write Unit Test**:
   Create `item_handler_test.go` using `httptest.NewRecorder()` and `gin.CreateTestContext()`.

### Recipe 2: Adding a New WebSocket Action

1. Open `internal/ws/admin_handler.go` (or create a domain-specific action handler).
2. Add a `case "your-action":` inside `AdminRoomHandler`:
   ```go
   case "your-action":
       // Process payload
       response := map[string]interface{}{"status": "success", "result": msg.Data}
       payloadBytes, _ := json.Marshal(response)
       _ = h.manager.Broadcast(ctx, h.roomName, "your-action-response", string(payloadBytes))
   ```
3. Update the embedded `web/admin.html` dashboard if the action should be testable or visible from the UI.

### Recipe 3: Ingesting & Querying Vectors (RAG)

1. **To Ingest a Document**:
   ```go
   embedding, err := llmClient.GenerateEmbedding(ctx, text)
   if err != nil { return err }
   doc := &pgstore.DocumentModel{
       Title:     title,
       Content:   text,
       Embedding: embedding,
   }
   err = docStore.CreateDocument(ctx, doc)
   ```
2. **To Perform Vector Similarity Search**:
   ```go
   queryEmbedding, _ := llmClient.GenerateEmbedding(ctx, query)
   matches, err := docStore.SearchSimilar(ctx, queryEmbedding, topK)
   ```
3. **To Stream an Answer Grounded on Retrieved Documents**:
   Format the retrieved documents into a context prompt and pass to `llmClient.GenerateChatCompletionStream`.

### Recipe 4: Registering a New Scheduled Job with Telemetry

In `cmd/server/main.go`:
```go
// Option A: Inline function registration with time.Duration interval
scheduler.RegisterFunc("analytics_sync", 1*time.Hour, func(ctx context.Context) error {
    // Perform periodic task logic
    log.Info("Running analytics sync...")
    return nil
})

// Option B: Implementing the jobs.Job interface
type SyncJob struct{}
func (j *SyncJob) Name() string { return "db_sync" }
func (j *SyncJob) Run(ctx context.Context) error { return nil }

scheduler.RegisterInterval(&SyncJob{}, 15*time.Minute)
```
* The custom scheduler isolates panics per job, captures call stacks with `runtime/debug.Stack()`, tracks `TaskInfo` (`RunCount`, `LastRun`, `LastDuration`, `Status`, `LastError`), and broadcasts updates directly to `/api/v1/jobs` and the `/admin` dashboard.

### Recipe 5: WebRTC P2P Signaling & Pion UDP DataChannel

1. **Fetch Configured ICE Servers**:
   Mobile and web peers request:
   `GET /api/v1/webrtc/ice-servers`
   Returns standard `urls` for STUN/TURN configurations.

2. **Establish P2P Call over simplysocket**:
   * **Join Room**: Client sends `{"action": "join-room", "message_body": {"room": "call-room-1"}}`.
   * **Announce Presence**: Client sends `{"action": "webrtc-join", "target": "call-room-1"}`.
   * **Exchange SDP Offer**:
     ```json
     {
       "action": "webrtc-offer",
       "target": "call-room-1",
       "message_body": {
         "target_id": "remote_peer_id",
         "sdp": { "type": "offer", "sdp": "v=0..." }
       }
     }
     ```
   * **Exchange SDP Answer**:
     ```json
     {
       "action": "webrtc-answer",
       "target": "call-room-1",
       "message_body": {
         "target_id": "initiating_peer_id",
         "sdp": { "type": "answer", "sdp": "v=0..." }
       }
     }
     ```
   * **Exchange ICE Candidates**:
     ```json
     {
       "action": "webrtc-ice",
       "target": "call-room-1",
       "message_body": {
         "target_id": "remote_peer_id",
         "candidate": { "candidate": "...", "sdpMid": "0", "sdpMLineIndex": 0 }
       }
     }
     ```

3. **Establish Server-Side WebRTC DataChannel (Pion)**:
   * Client creates `RTCPeerConnection` with a DataChannel (`label: "echo"`).
   * Generates SDP offer and posts to `POST /api/v1/webrtc/server/session`.
   * Server returns negotiated SDP answer with all ICE candidates already gathered.
   * Client sets remote description -> DataChannel opens over UDP!
   * Send ping: `serverDataChannel.send("ping:" + Date.now())` -> server replies with `pong:<timestamp>` for sub-millisecond round-trip latency benchmarking.

### Recipe 6: Pion SFU (Selective Forwarding Unit) Multi-Party Group Conferencing

For multi-client group calls (3+ participants), mesh P2P exhausts mobile uplink bandwidth. The built-in Pion SFU routes raw RTP packets with $O(1)$ uplink bandwidth per peer:

1. **Join SFU Conference Room & Ingest Tracks**:
   * Client creates `RTCPeerConnection` with local video/audio tracks.
   * Adds `recvonly` transceivers for receiving downlink streams.
   * Creates SDP offer and POSTs to `/api/v1/webrtc/sfu/join`:
     ```json
     {
       "room_id": "conf-alpha",
       "peer_id": "mobile-alice",
       "sdp": "v=0..."
     }
     ```
   * SFU attaches existing room tracks, binds an RTP forwarding loop, launches periodic RTCP Picture Loss Indication (PLI) keyframe requests, and returns the SDP answer:
     ```json
     {
       "status": "connected",
       "room_id": "conf-alpha",
       "peer_id": "mobile-alice",
       "answer": { "type": "answer", "sdp": "v=0..." },
       "active_peers": 3,
       "active_tracks": 4
     }
     ```

2. **Dynamic Downlink Renegotiation**:
   * When a new peer joins and publishes a track, the SFU triggers `onTrackHook`, broadcasting `sfu-track-published` over `simplysocket`:
     ```json
     {
       "action": "sfu-track-published",
       "room_id": "conf-alpha",
       "publisher_id": "mobile-bob",
       "track_kind": "video"
     }
     ```
   * Connected subscribers create a renegotiation offer and POST to `/api/v1/webrtc/sfu/renegotiate`:
     ```json
     {
       "room_id": "conf-alpha",
       "peer_id": "mobile-alice",
       "sdp": "v=0..."
     }
     ```
   * The new remote track fires `pc.ontrack` on the client, rendering the new participant's video tile without disturbing ongoing streams.

3. **Graceful Teardown**:
   * Client calls `POST /api/v1/webrtc/sfu/leave` with `{"room_id": "conf-alpha", "peer_id": "mobile-alice"}`.
   * SFU closes peer senders, removes published tracks, and deletes the room when empty.

4. **Cluster Observability**:
   * `GET /api/v1/webrtc/sfu/rooms`: Returns active rooms, peer count, participant IDs, and track count for admin telemetry.

### Recipe 7: User Base & Multi-Level RBAC Authentication

1. **User Model & Metadata**:
   * Stored in PostgreSQL `users` table with bcrypt password hashing and `metadata` JSONB column.
   * `metadata` embeds authorization attributes (`auth_level`, `role`, `department`, `permissions`).
   * When PostgreSQL is disabled or offline, falls back seamlessly to an in-memory thread-safe store.
   * Default seeded accounts: `admin` (auth_level: 99) and `demo_user` (auth_level: 1).

2. **Issuing & Validating Tokens**:
   ```go
   // Generate signed token with user metadata & auth_level embedded in claims
   token, err := middleware.GenerateUserToken(cfg.JWTSecret, user, cfg.TokenTTL)
   ```

3. **Protecting Routes with Multi-Level RBAC**:
   ```go
   // Restrict endpoint to users with auth_level >= 50
   v1.GET("/protected/admin-only",
       middleware.JWTAuth(cfg.JWTSecret),
       middleware.RequireAuthLevel(50),
       handlerFunc,
   )

   // Or restrict by role
   v1.GET("/protected/moderators",
       middleware.JWTAuth(cfg.JWTSecret),
       middleware.RequireRole("admin", "moderator"),
       handlerFunc,
   )
   ```

4. **Testing in Admin UI**:
   * Navigate to `/admin`.
   * Under **User Base & Multi-Level Auth (RBAC)**, click **"🔑 Get JWT"** on any account.
   * The token, claims, and auth level are instantly loaded into the inspector bench.
   * Click **"GET /protected/admin-only"** to verify access granted or 403 Forbidden based on `auth_level`.

5. **WebSocket Room-Level RBAC**:
   * The connection endpoint (`/api/v1/ws`) is open to all clients.
   * Fine-grained room admission (such as the `"admin"` room requiring Level 10–99) is enforced inside simplysocket room handlers (`EventsRoomHandler`), verifying the client's token before admitting them to the room.

---

## 4. Mobile Team Integration Guide

When pairing with the mobile developers (iOS / Android / Flutter):
1. **Network Binding**: Ensure the server runs on `0.0.0.0:8080`.
   * Android Emulators connect to `http://10.0.2.2:8080` (or `ws://10.0.2.2:8080/ws`).
   * iOS Simulators connect to `http://localhost:8080` (or `ws://localhost:8080/ws`).
   * Physical Devices connect to `http://<LAN-IP>:8080` or via `ngrok http 8080`.
2. **Unified WebSocket Envelope**:
   ```json
   {
     "action": "join-room | admin-broadcast | llm-stream-request | webrtc-join | webrtc-offer | webrtc-answer | webrtc-ice",
     "room": "room-name",
     "sender": "client-id",
     "data": "payload or stringified json"
   }
   ```
3. **Debug Dashboard**: Direct mobile devs to `http://<HOST>:8080/admin` to inspect real-time connection status, test message delivery, monitor scheduled jobs, and run interactive WebRTC P2P video calls and DataChannel benchmarks.

---

## 5. Verification & Testing Playbook

Before completing any task, execute the following validation commands:

```bash
# 1. Run all unit tests with data race detector
go test -v -race ./...

# 2. Verify static analysis and formatting
go vet ./...

# 3. Compile the production binary
make build

# 4. Verify binary starts cleanly (smoke test)
./bin/server &
PID=$!
sleep 2
curl -s http://localhost:8080/health
kill $PID
```

