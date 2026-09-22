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
* **HTTP Router**: `go-chi/chi/v5` with sub-routing and middleware chains.
* **Database**: PostgreSQL 16 + `pgvector` extension (`vector(1536)` embeddings).
* **Database Access**: `jmoiron/sqlx` with native SQL queries.
* **Cache**: `redis/go-redis/v9` with standard key-value and expiration methods.
* **Real-time Engine**: [`simplysocket`](https://github.com/DhruvikDonga/simplysocket) mesh WebSocket server supporting multi-room multiplexing.
* **AI & RAG Engine**: Native vector store with HNSW cosine distance indexing (`<=>`), LLM token streaming, and zero-key offline mock fallback.
* **Background Scheduler**: `robfig/cron/v3` with runtime task telemetry tracking.
* **Logging**: `uber-go/zap` structured logging.
* **Admin Dashboard**: Zero-dependency single-page UI embedded directly via Go `embed.FS` at `/admin` and `/`.

### Directory Layout

```text
hack-go-thon/
├── cmd/server/main.go            # Application entrypoint & dependency injection
├── config/config.go              # Environment variable loading & defaults
├── internal/
│   ├── api/
│   │   ├── router.go             # Chi HTTP router & route mounts
│   │   └── handler/              # HTTP handlers (health, user, rag, etc.)
│   ├── store/
│   │   ├── store.go              # Storage interfaces (UserStore, DocumentStore)
│   │   └── pg_store/             # PostgreSQL + pgvector implementations
│   ├── ws/
│   │   ├── manager.go            # simplysocket Manager wrapper & broadcast safety
│   │   ├── handler.go            # WebSocket connection upgrade & client registration
│   │   └── admin_handler.go      # Admin room handler & LLM token streaming
│   ├── llm_client/
│   │   └── client.go             # LLM completions, streaming & embeddings (w/ offline fallback)
│   └── jobs/
│       └── scheduler.go          # robfig/cron runner with TaskInfo observability
├── pkg/                          # Shared reusable packages (logger, cache, utils)
└── web/
    ├── web.go                    # Go embed.FS declaration
    └── admin.html                # Embedded dark-mode admin control center
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
   In `internal/store/store.go`, declare the model struct with `db` and `json` tags, and add methods to the relevant store interface:
   ```go
   type Item struct {
       ID        string    `json:"id" db:"id"`
       Title     string    `json:"title" db:"title"`
       CreatedAt time.Time `json:"created_at" db:"created_at"`
   }
   type ItemStore interface {
       CreateItem(ctx context.Context, item *Item) error
       GetItem(ctx context.Context, id string) (*Item, error)
   }
   ```
2. **Implement in PostgreSQL Store**:
   In `internal/store/pg_store/item.go`, implement the methods using `r.db.NamedExecContext` or `r.db.GetContext`.
3. **Create the HTTP Handler**:
   In `internal/api/handler/item_handler.go`, create `ItemHandler` with standard JSON encoding/decoding.
4. **Mount in Chi Router**:
   In `internal/api/router.go`, add route under `/api/v1/items`.
5. **Write Unit Test**:
   Create `item_handler_test.go` using `net/http/httptest`.

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
   doc := &store.Document{
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
In `cmd/server/main.go` or a job setup module:
```go
cronScheduler.RegisterTask("analytics_sync", "@every 1h", func() error {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    // Perform periodic task logic
    return nil
})
```
* The job is automatically tracked in `cronScheduler.GetTasks()` and immediately visible in the `/admin` UI table under Scheduled Jobs.

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
     "action": "join-room | admin-broadcast | llm-stream-request",
     "room": "room-name",
     "sender": "client-id",
     "data": "payload or stringified json"
   }
   ```
3. **Debug Dashboard**: Direct mobile devs to `http://<HOST>:8080/admin` to inspect real-time connection status, test message delivery, and monitor system events.

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

