# Hack-Go-Thon Boilerplate Documentation

Welcome to the comprehensive technical documentation for the **Hack-Go-Thon** base boilerplate. This boilerplate is designed for rapid hackathon velocity while adhering to production-grade microservice architecture, strict type safety, clean separation of concerns, and resilient concurrency.

---

## Table of Contents

1. [Architectural Overview](#1-architectural-overview)
2. [Project Layout](#2-project-layout)
3. [Configuration System](#3-configuration-system)
4. [Structured Logging](#4-structured-logging)
5. [Error Handling & Standard Responses](#5-error-handling--standard-responses)
6. [HTTP API & Routing Engine](#6-http-api--routing-engine)
7. [Authentication & Security Middlewares](#7-authentication--security-middlewares)
8. [Real-time WebSockets (`simplysocket`)](#8-real-time-websockets-simplysocket)
9. [Database Client & PostgreSQL Store](#9-database-client--postgresql-store)
10. [LLM Client Integration](#10-llm-client-integration)
11. [Background Job Scheduling & Workers](#11-background-job-scheduling--workers)
12. [Health Checks & Observability](#12-health-checks--observability)
13. [Graceful Lifecycle & Shutdown](#13-graceful-lifecycle--shutdown)
14. [Docker & Containerization](#14-docker--containerization)
15. [Recipes & Common Extensions](#15-recipes--common-extensions)
16. [WebRTC Real-Time Media & Pion DataChannels](#16-webrtc-real-time-media--pion-datachannels)
17. [Mobile Webhook Notification System](#17-mobile-webhook-notification-system)
18. [Multipart Form File Upload API](#18-multipart-form-file-upload-api)

---

## 1. Architectural Overview

```
                           ┌─────────────────────────────────────────┐
                           │    Client (HTTP / WS / WebRTC Media)    │
                           └───────┬─────────────────┬───────────────┘
                                   │                 │
                ┌──────────────────▼──────────┐      │  [Direct UDP Media / RTP]
                │      Gin Router Engine      │      │
                │   (CORS, Request ID, Zap)   │      │
                └──────────────┬──────────────┘      │
                               │                     │
     ┌─────────────────────────┼─────────────────────┼─────────────────────────┐
     │                         │                     │                         │
┌────▼──────────────┐ ┌────────▼───────────┐ ┌───────▼───────────┐ ┌───────────▼───────────┐
│  Public Endpoints │ │   JWT Protected    │ │ API Key Protected │ │ WebRTC & Media Routes │
│ (/health, /items) │ │    (/protected)    │ │     (/secure)     │ │ (/api/v1/webrtc/*)    │
└────┬──────────────┘ └────────┬───────────┘ └───────┬───────────┘ └───────────┬───────────┘
     │                         │                     │                         │
     └─────────────────────────┼─────────────────────┴─────────────────────────┘
                               │
       ┌───────────────────────┼───────────────────────┬───────────────────────┐
       │                       │                       │                       │
┌──────▼───────────┐    ┌──────▼───────────┐    ┌──────▼───────────┐    ┌──────▼───────────┐
│ PostgreSQL Store │    │   simplysocket   │◄───┤ Pion WebRTC SFU  │    │  Job Scheduler   │
│  (Items, Keys,   │    │  WebSocket Mesh  │event│  & DataChannel   │    │ (In-Process Cron,│
│ Audit, pgvector) │    │(P2P Signaling,rd)│hook│(RTP Fan-Out, PLI)│    │ Workers, Panics) │
└──────────────────┘    └──────────────────┘    └──────┬───────────┘    └──────────────────┘
                                                       │
                                                ┌──────▼───────────┐
                                                │    LLM Client    │
                                                │ (OpenAI SDK, RAG │
                                                │ Embeds, Fallback)│
                                                └──────────────────┘
```

The system is organized into modular, independently toggleable components:
- **HTTP Routing Layer (Gin)**: High-performance router with CORS, request correlation (`X-Request-ID`), Zap access logging, panic recovery, and asynchronous PostgreSQL audit logging.
- **WebSocket Mesh (`simplysocket`)**: Single connection endpoint (`/api/v1/ws`) multiplexing independent `RoomData` handlers for admin telemetry, LLM token streaming, and WebRTC P2P signaling.
- **WebRTC Subsystem (Pion & `simplysocket`)** *(see [WEBRTC_INFO.md](WEBRTC_INFO.md))*:
  - **1:1 P2P Mesh**: Direct browser-to-browser audio/video calls with signaling coordinated over `simplysocket`.
  - **Selective Forwarding Unit (SFU)**: Enterprise-grade media router with $O(1)$ client uplink bandwidth, raw RTP track forwarding, and periodic RTCP PLI keyframe heartbeats (`/api/v1/webrtc/sfu/*`).
  - **Server DataChannel**: Sub-millisecond direct UDP binary messaging and latency ping-pong benchmarks (`/api/v1/webrtc/server/session`).
  - **Cross-Subsystem Event Hook**: SFU track publishing triggers live notification broadcasts across the `simplysocket` mesh.
- **PostgreSQL & pgvector**: ACID relational persistence and 1536-dimensional HNSW cosine vector search for grounded RAG Q&A.
- **In-Process Job Scheduler**: Resilient periodic cron scheduler with panic recovery, runtime metrics, and live observability (`GET /api/v1/jobs`).
- **Subsystem Feature Flags (`services.json`)**: Granular toggles allowing developers to selectively enable or disable components with zero overhead.

---

## 2. Project Layout

```
hack-go-thon/
├── cmd/
│   └── server/
│       └── main.go                 # Application bootstrap & lifecycle orchestration
├── config/
│   ├── config.go                   # Strongly typed environment configuration
│   ├── services.go                 # Subsystem feature flags loader (services.json)
│   └── services_test.go            # Unit tests for services configuration & aliases
├── services.json                   # Optional JSON config to selectively toggle subsystems
├── internal/
│   ├── api/
│   │   ├── handler/
│   │   │   ├── health_handler.go   # Liveness & Readiness probe handlers
│   │   │   ├── example_handler.go  # Sample REST handler (Validation, DB, LLM)
│   │   │   ├── rag_handler.go      # pgvector RAG document ingestion & semantic search
│   │   │   └── webrtc_handler.go   # WebRTC ICE servers, DataChannel & SFU endpoints
│   │   ├── middleware/
│   │   │   ├── api_call_logger.go  # Asynchronous PostgreSQL audit logging
│   │   │   ├── api_key_auth.go     # X-API-Key validator (DB or master key)
│   │   │   ├── cors.go             # CORS configuration
│   │   │   ├── jwt_auth.go         # Bearer JWT token validator & token generator
│   │   │   ├── logger.go           # Zap HTTP access logger with X-Request-ID
│   │   │   └── recovery.go         # Panic recovery returning structured JSON 500
│   │   ├── router.go               # Gin routing engine & endpoint registry
│   │   └── server.go               # HTTP server lifecycle wrapper
│   ├── db_client/
│   │   └── postgresql_client.go    # PostgreSQL pool client with health check probe
│   ├── jobs/
│   │   ├── job.go                  # Job interface & JobFunc adapter
│   │   ├── scheduler.go            # Periodic job scheduler with panic recovery
│   │   └── example_job.go          # Concrete heartbeat scheduled task
│   ├── llm_client/
│   │   └── client.go               # OpenAI SDK client wrapper & chat completion helper
│   ├── store/
│   │   └── pg_store/
│   │       ├── api_calls.go        # API audit logging schema & inserts
│   │       ├── api_keys.go         # API keys schema & lookups
│   │       ├── documents.go        # pgvector vector store & similarity search
│   │       └── item.go             # PostgreSQL item schema migration & CRUD repository
│   ├── webrtc_server/
│   │   ├── server.go               # Pion WebRTC server peer manager & UDP DataChannel
│   │   └── sfu.go                  # Pion SFU multi-party media router & RTP forwarder
│   ├── worker/
│   │   └── worker.go               # Continuous background worker processor
│   └── ws/
│       ├── admin_handler.go        # Admin dashboard telemetry & LLM token streaming
│       ├── chat_handler.go         # ChatRoomHandler implementing simplysocket.RoomData
│       ├── handler.go              # EventsRoomHandler implementing simplysocket.RoomData
│       ├── manager.go              # WebSocket mesh manager & Gin upgrade handler
│       └── webrtc_handler.go       # WebRTC P2P signaling RoomData handler
├── pkg/
│   ├── apperrors/
│   │   └── errors.go               # Standard domain errors & HTTP status code mapping
│   ├── log/
│   │   ├── logger.go               # Global & contextual structured logging
│   │   └── zap_config.go           # Development (console) vs Production (JSON) Zap config
│   └── response/
│       └── response.go             # Standardized JSON response envelope
├── web/
│   ├── admin.html                  # Embedded dark-mode control center UI with WebRTC lab
│   └── web.go                      # Go embed.FS declaration
├── .dockerignore
├── .env.example
├── .gitignore
├── Dockerfile                      # Multi-stage minimal Alpine container build
├── docker-compose.yml              # Local container deployment (App + PostgreSQL)
├── Makefile                        # Common developer task automation
├── go.mod
├── go.sum
└── README.md
```

---

## 3. Configuration System

Application configuration is loaded from environment variables into a strongly typed struct in `config/config.go`, combined with selective subsystem toggling via `services.json`.

### Supported Variables

| Variable | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `APP_NAME` | string | `hack-go-thon` | Name of the service |
| `PORT` | string | `:8080` | Port for the HTTP and WebSocket server |
| `LOG_LEVEL` | string | `debug` | Minimum log level (`debug`, `info`, `warn`, `error`) |
| `ENV` | string | `development` | `development` (color console) or `production` (JSON) |
| `SHUTDOWN_TIMEOUT_SECONDS`| int | `10` | Seconds to wait for in-flight requests during SIGINT/SIGTERM |
| `CORS_ALLOWED_ORIGINS` | string | `*` | Comma-separated allowed origins or `*` for all |
| `PG_URI` / `DATABASE_URL` | string | `""` | PostgreSQL connection string |
| `OPENAI_API_KEY` | string | `""` | API key for OpenAI LLM client |
| `JWT_SECRET` | string | `default-dev-jwt-secret` | HMAC-SHA256 secret for signing and validating JWTs |
| `MASTER_API_KEY` | string | `""` | Optional superuser API key bypassing DB lookup |
| `STUN_SERVERS` | string | Google STUN | Comma-separated STUN server URLs for WebRTC |
| `TURN_SERVER_URL` | string | `""` | Optional TURN relay server URL |
| `TURN_USERNAME` | string | `""` | TURN authentication username |
| `TURN_CREDENTIAL` | string | `""` | TURN authentication credential |
| `SERVICES_CONFIG_PATH` | string | `services.json` | Path to JSON file toggling subsystem initialization |

### Subsystem Feature Flags (`services.json`)

Granular subsystem toggling allows disabling unneeded components without code modifications. All services default to `true`. When set to `false`, the subsystem is completely excluded from startup (no background goroutines, connection pools, or route handlers are created).

```json
{
  "database": true,
  "api_handler": true,
  "rag_handler": true,
  "job_scheduler": true,
  "websocket": true,
  "webrtc": true
}
```

#### Supported Subsystems & Lenient Aliases

| Flag | Supported Aliases | Subsystem Affected |
| :--- | :--- | :--- |
| `database` | `db`, `postgres` | PostgreSQL connection pool, schema migrations, and API call audit logger |
| `api_handler` | `api`, `example_handler`, `apihandler` | Example CRUD `/api/v1/items` and AI completions `/api/v1/llm/ask` |
| `rag_handler` | `rag`, `raghandler` | pgvector document ingestion and similarity search `/api/v1/rag/*` |
| `job_scheduler` | `scheduler`, `jobs`, `jobscheduler` | In-process periodic cron engine, heartbeat tasks, and `/api/v1/jobs` |
| `websocket` | `websockets`, `ws` | `simplysocket` multi-room real-time mesh hub at `/api/v1/ws` |
| `webrtc` | `webrtc_server`, `sfu` | Pion P2P server sessions and SFU multi-party video conferencing at `/api/v1/webrtc/*` |

### Code Usage
```go
import "hack-go-thon/config"

cfg := config.Load()
fmt.Println(cfg.Port, cfg.Services.WebRTC, cfg.Services.WebSocket)
```

---

## 4. Structured Logging

Powered by Uber's `go.uber.org/zap`, the logging system in `pkg/log` offers structured key-value logging, stack traces on errors, and environment-aware formatting:
- **`development` mode**: Colorized, tab-separated, human-readable console output.
- **`production` mode**: High-throughput structured JSON output suitable for Datadog, CloudWatch, or Grafana Loki.

### Global Helpers
```go
import "hack-go-thon/pkg/log"

log.Debug("Debugging cache lookup", "key", "user:123", "hits", 4)
log.Info("Order placed", "order_id", "ord_999", "amount", 49.99)
log.Warn("Rate limit threshold approached", "client_ip", "1.2.3.4")
log.Error("Database transaction failed", "error", err.Error())
log.Fatal("Critical startup dependency unavailable") // Logs and calls os.Exit(1)
```

### Contextual Sub-Loggers
```go
subLog := log.With("module", "payments", "tenant_id", "tenant_42")
subLog.Infow("Processing payment batch", "batch_size", 100)
```

---

## 5. Error Handling & Standard Responses

### Standard Response Envelope (`pkg/response`)

Every HTTP API response from the boilerplate uses a standardized JSON envelope:

#### Success Envelope
```json
{
  "success": true,
  "data": {
    "id": "item_12345",
    "name": "Widget"
  },
  "meta": { "page": 1, "total": 50 } // Optional
}
```

#### Error Envelope
```json
{
  "success": false,
  "error": {
    "code": "BAD_REQUEST",
    "message": "Invalid request payload",
    "details": "field 'name' is required"
  }
}
```

### Domain Error System (`pkg/apperrors`)

Construct custom errors with explicit HTTP status codes, machine-readable error codes, and underlying error wrapping:

```go
// Bad Request (400)
apperrors.NewBadRequest("Validation failed", map[string]string{"name": "required"})

// Not Found (404)
apperrors.NewNotFound("User with ID 123 not found")

// Unauthorized (401)
apperrors.NewUnauthorized("Invalid or expired session token")

// Forbidden (403)
apperrors.NewForbidden("You lack permission to perform this action")

// Conflict (409)
apperrors.NewConflict("Email address already registered")

// Internal Server Error (500)
apperrors.NewInternal("Database query execution failed", originalErr)
```

### In Handlers
```go
func MyHandler(c *gin.Context) {
    var req RequestPayload
    if err := c.ShouldBindJSON(&req); err != nil {
        response.Error(c, apperrors.NewBadRequest("Invalid payload", err.Error()))
        return
    }

    result, err := service.Process(req)
    if err != nil {
        response.Error(c, apperrors.NewInternal("Failed processing", err))
        return
    }

    response.OK(c, result)
}
```

---

## 6. HTTP API & Routing Engine

The boilerplate uses **Gin** configured with a standard middleware pipeline:

1. **CORS Middleware**: Handles cross-origin requests, custom headers, and `OPTIONS` preflight requests.
2. **APILogger Middleware**: Assigns a unique `X-Request-ID` (or honors an existing one), timing the request and logging status, method, path, IP, and latency via Zap.
3. **Recovery Middleware**: Traps any unhandled panics, prints a formatted stack trace in Zap, and outputs a 500 JSON error envelope without dropping connections.
4. **APICallLogger**: Writes an audit log record into PostgreSQL `api_calls` asynchronously if a database is connected.

### Versioned Routing (`internal/api/router.go`)

Routes are registered under the `/api/v1` namespace:

```go
v1 := engine.Group("/api/v1")
{
    // Health probes
    v1.GET("/health/live", rc.HealthHandler.Live)
    v1.GET("/health/ready", rc.HealthHandler.Ready)

    // WebSocket endpoint
    v1.GET("/ws", rc.WSManager.Handler())

    // Resource endpoints
    items := v1.Group("/items")
    {
        items.POST("", rc.ExampleHandler.CreateItem)
        items.GET("", rc.ExampleHandler.ListItems)
        items.GET("/:id", rc.ExampleHandler.GetItem)
    }
}
```

---

## 7. Authentication & Security Middlewares

### 1. Pre-Setup User Base & Multi-Level Auth (RBAC)
The boilerplate includes a pre-configured user model (`internal/store/pg_store/users.go`) and REST handler (`internal/api/handler/user_handler.go`) designed for products requiring multi-tier access without third-party identity providers:

#### UserModel Schema
```go
type UserModel struct {
    ID           string         `json:"id"`
    Username     string         `json:"username"`
    Email        string         `json:"email"`
    PhoneNumber  string         `json:"phone_number"`
    PasswordHash string         `json:"-"`
    Metadata     map[string]any `json:"metadata"`
    CreatedAt    time.Time      `json:"created_at"`
    UpdatedAt    time.Time      `json:"updated_at"`
}
```

- **Password Hashing**: Secure bcrypt hashing (`pgstore.HashPassword` & `pgstore.CheckPassword`).
- **Flexible JSONB Metadata**: Holds arbitrary key-values including `auth_level`, `role`, `department`, and `permissions`.
- **Pre-Seeded Accounts**:
  - `admin` (`admin@hack-go-thon.local` / `Mp@tel98`, phone `9427425572`): `auth_level: 99`, `role: "admin"` — full read/write, user management, and `admin` room access.
  - `staff` (`staff@hack-go-thon.local` / `Staff@123`, phone `9427425570`): `auth_level: 10`, `role: "staff"` — view-only dashboard access. Restricted from room `"admin"` and user modifications.
  - `demo_user` (`user@hackathon.local` / `user123`, phone `+1-555-0101`): `auth_level: 1`, `role: "member"`
- **Access Control & Room-Level Security**:
  - The Admin Panel (`/admin`) UI prompts for login requiring an authenticated account with **Auth Level 10 to 99**.
  - **Level 10 View-Only Restrictions**: Accounts with Auth Level $\le 10$ have read-only access to user directories (`GET /api/v1/users`, `GET /api/v1/users/:id`). Modification endpoints (`POST /api/v1/users`, `PUT /api/v1/users/:id`, `DELETE /api/v1/users/:id`) respond with `403 Forbidden`. The Admin UI adaptively hides or disables user creation, update, and deletion controls.
  - The WebSocket URL (`/api/v1/ws`) is an open connection endpoint allowing any client to connect to `mesh-global`.
  - Room-level RBAC is enforced within simplysocket: only users with **Auth Level strictly above 10 (Level 11 to 99)** can join the `"admin"` room. Non-admin join attempts are rejected with `"Unauthorized: admin chat room requires Auth Level above 10 (Level 11 to 99)"`.
- **Dual Storage Engine**: Persists to PostgreSQL `users` table with `JSONB` index. When PostgreSQL is offline or disabled (`services.json`), seamlessly falls back to thread-safe in-memory storage so demo velocity is never blocked.

#### Auth & User Endpoints
| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/v1/auth/register` | Register new user with password hashing & metadata, returns signed JWT |
| `POST` | `/api/v1/auth/login` | Authenticate with username/email & password, returns JWT & user profile |
| `GET` | `/api/v1/auth/me` | Retrieve profile and decoded JWT claims for authenticated user |
| `GET` | `/api/v1/users` | List all registered users |
| `POST` | `/api/v1/users` | Admin create user with full metadata control |
| `GET` | `/api/v1/users/:id` | Get user by ID |
| `PUT` | `/api/v1/users/:id` | Update phone number, password, or metadata |
| `DELETE` | `/api/v1/users/:id` | Delete user by ID |
| `POST` | `/api/v1/users/:id/token` | Instant JWT generator for user without requiring password (for Admin UI testing) |

### 2. JWT Claims with Embedded Metadata (`internal/api/middleware/jwt_auth.go`)
User metadata and `auth_level` are directly embedded into the signed JWT payload claims:

```go
type JWTClaims struct {
    UserID      string         `json:"user_id"`
    Username    string         `json:"username,omitempty"`
    Email       string         `json:"email"`
    PhoneNumber string         `json:"phone_number,omitempty"`
    Role        string         `json:"role,omitempty"`
    AuthLevel   any            `json:"auth_level,omitempty"`
    Metadata    map[string]any `json:"metadata,omitempty"`
    jwt.RegisteredClaims
}
```

When verified, the middleware automatically injects into `gin.Context`:
- `c.GetString("user_id")`
- `c.GetString("username")`
- `c.GetString("email")`
- `c.GetString("phone_number")`
- `c.GetString("role")`
- `c.MustGet("auth_level")`
- `c.MustGet("metadata")`

### 3. Multi-Level Authorization Middlewares
Protect endpoints with custom authorization requirements without database lookups:

```go
// 1. Require minimum authorization level (e.g. >= 50)
v1.GET("/protected/admin-only", 
    middleware.JWTAuth(cfg.JWTSecret), 
    middleware.RequireAuthLevel(50), 
    func(c *gin.Context) {
        response.OK(c, gin.H{"status": "authorized"})
    },
)

// 2. Require specific role membership
v1.GET("/protected/staff", 
    middleware.JWTAuth(cfg.JWTSecret), 
    middleware.RequireRole("admin", "moderator"), 
    handlerFunc,
)
```

### 4. API Key Authentication (`internal/api/middleware/api_key_auth.go`)
- Inspects `X-API-Key` header.
- Validates against PostgreSQL `api_keys` table using `pgstore.GetAPIKeyBySecret`.
- Supports an optional `cfg.MasterAPIKey` bypass (useful for administrative services or internal tooling).
- Injects `user_id`, `api_key_id`, and `api_key_name` into context.

```go
secureGroup := v1.Group("/secure")
secureGroup.Use(middleware.APIKeyAuth(db, cfg.MasterAPIKey))
{
    secureGroup.GET("/data", MySecureHandler)
}
```

### 5. PostgreSQL API Call Audit Logger (`internal/api/middleware/api_call_logger.go`)
- Intercepts requests and records:
  - `user_id` (if authenticated via JWT or API Key)
  - Endpoint path & HTTP method
  - Response status code
  - Execution latency in milliseconds
  - Client IP
- Executed in an asynchronous background goroutine with a 5-second timeout, adding **zero milliseconds** to user response time.

---

## 8. Real-time WebSockets (`simplysocket`) & Admin Control Center

The boilerplate embeds [`github.com/DhruvikDonga/simplysocket`](https://github.com/DhruvikDonga/simplysocket), implementing the **"Connect once, write logic multiple times"** architecture.

### Why This Architecture?
Clients open a single WebSocket connection to `/api/v1/ws`. Instead of writing monolithic routers or spinning up multiple WebSocket ports, developers implement the simple `simplysocket.RoomData` interface for different features.

```go
type RoomData interface {
    HandleRoomData(room simplysocket.Room, server simplysocket.MeshServer)
}
```

### Reference Handlers Provided:

#### 1. `ws.AdminRoomHandler` (`internal/ws/admin_handler.go`)
- **Dynamic Room Lifecycle**: Assigned to room `"admin"` when created dynamically upon the first authorized admin join.
- **Mesh Observability**: Periodically and reactively queries `server.GetRooms()`, `server.GetClients()`, and `server.GetClientsInRoom()`, broadcasting an `admin-state` snapshot to all clients in the `"admin"` room.
- **simplysocket LLM Token Streaming**: Receives `llm-stream-request`, invokes `llmClient.GenerateChatCompletionStream`, and streams tokens chunk-by-chunk to the room using `llm-stream-chunk` messages.
- **Direct Room Broadcast**: Dispatches incoming room messages (`action: "broadcast"`, `target: "admin"`) directly to room occupants via `room.BroadcastMessage(msg)`.

#### 2. `ws.EventsRoomHandler` (`internal/ws/handler.go`)
- **Default Lobby Handler (`mesh-global`)**: Serves as the default handler for simplysocket's initial lobby room (`simplysocket.MeshGlobalRoom`), where all connecting clients land automatically.
- **Direct Mesh Broadcast**: Clients of all roles and authorization levels (Admin, Staff, or Member) can broadcast directly to `mesh-global` (`action: "broadcast"`, `target: "mesh-global"`). `EventsRoomHandler` immediately delivers it to all connected clients via `room.BroadcastMessage(msg)`.
- **Dynamic Room Joining & RBAC**: Clients in `mesh-global` send `action: "join-room"`. Joining the special `"admin"` room strictly requires Auth Level $> 10$ (Level 11 to 99), verified via `middleware.VerifyTokenAuthLevel`.
- **Live Mesh Telemetry**: Live endpoint `GET /api/v1/ws/rooms` unions `MeshServer.GetRooms()` and `MeshServer.GetClientsInRoom()`, providing real-time room names and connected client slugs.
- **Ping / Pong**: Automatically answers `action: "ping"` with a timestamped `"pong"`.
- **Client Lifecycle**: Listens to `room.EventTriggers()` for `client-joined-room` and `client-left-room` events.
- **Heartbeat**: Broadcasts a background tick every 30 seconds.

#### 3. `ws.ChatRoomHandler` (`internal/ws/chat_handler.go`)
- Dedicated group chat logic: handles `"send-chat"`, `"broadcast"`, and `"user-typing"` indicators.

### Embedded Admin Dashboard UI
The server serves a responsive, dark-mode single-page control center directly at:
- `http://localhost:8080/admin` (and `http://localhost:8080/`)
- Built with zero frontend build dependencies (embedded directly into Go binary via `web/web.go`).
- Visualizes mesh rooms, active clients, live event logs, real-time LLM streaming, and pgvector RAG queries.
- Includes a dedicated **Mesh Broadcast Tool** allowing direct broadcasting to `mesh-global` (all users) or any active room.

---

## 9. Database Client, PostgreSQL Store & pgvector RAG

### Database Client (`internal/db_client/postgresql_client.go`)
- Manages an `*sql.DB` connection pool using `github.com/lib/pq`.
- Configured connection limits:
  - Max Open Connections: `25`
  - Max Idle Connections: `5`
  - Connection Max Lifetime: `5 minutes`
- Implements `handler.Checker` interface (`Name()` and `Check(ctx)`), linking automatically into readiness probes.

### PostgreSQL Store Layer (`internal/store/pg_store/`)

The repository pattern is structured into dedicated domain files:

1. **`documents` Table with `pgvector` (`documents.go`)**:
   - `InitDocumentSchema(ctx, db)`: Enables PostgreSQL `vector` extension and creates `documents` table with `embedding vector(1536)` and an **HNSW cosine index** (`vector_cosine_ops`).
   - `CreateDocument(ctx, db, doc)`: Serializes float32 embeddings into Postgres vector literal format `[v1,v2,...]` and persists document records.
   - `SearchSimilarDocuments(ctx, db, embedding, limit)`: Performs high-speed semantic nearest neighbor search using the cosine distance operator `<=>`:
     ```sql
     SELECT id, title, content, metadata, 1 - (embedding <=> $1) AS similarity
     FROM documents
     ORDER BY embedding <=> $1
     LIMIT $2;
     ```
   - `ListDocuments(ctx, db, limit, offset)`: Paginated document retrieval.
2. **`items` Table (`item.go`)**:
   - `InitSchema(ctx, db)`: Auto-creates `items` table on startup (`id`, `title`, `description`, `status`, `created_at`, `updated_at`).
   - `CreateItem(ctx, db, item)`: Inserts a new item.
   - `GetItemByID(ctx, db, id)`: Selects by primary key.
   - `ListItems(ctx, db, limit, offset)`: Paginated query with limit & offset.
   - `DeleteItem(ctx, db, id)`: Deletes record by ID.
3. **`api_keys` Table (`api_keys.go`)**:
   - `InitAPIKeySchema(ctx, db)`: Auto-creates `api_keys` table.
   - `CreateAPIKey(ctx, db, key)`: Generates a new API key record.
   - `GetAPIKeyBySecret(ctx, db, secret)`: Lookup by token.
4. **`api_calls` Table (`api_calls.go`)**:
   - `InitAPICallSchema(ctx, db)`: Auto-creates audit table.
   - `InsertAPICall(ctx, db, call)`: Records API call metrics.

### RAG REST Endpoints (`internal/api/handler/rag_handler.go`)
- `POST /api/v1/rag/documents`: Uploads text, generates 1536-dimensional vector embedding, and stores in PostgreSQL.
- `GET /api/v1/rag/documents`: Lists indexed knowledge base documents.
- `POST /api/v1/rag/search`: Semantic vector similarity search. Returns closest document chunks ranked by cosine similarity.
- `POST /api/v1/rag/ask`: End-to-end RAG question answering. Retrieves relevant chunks from pgvector, synthesizes an augmented prompt, queries LLM, and returns the response with document citations.

---

## 10. LLM Client Integration & simplysocket Streaming

Located in `internal/llm_client/client.go`, the boilerplate provides a clean wrapper around the official `github.com/openai/openai-go` SDK with fallback mock support for zero-friction hackathon demos.

### 1. Synchronous Chat Completion
```go
client := llmclient.NewClient(cfg.OpenAIKey)
response, err := client.GenerateChatCompletion(ctx, "gpt-4o-mini", "Summarize this article")
```

### 2. Real-Time Token Streaming
```go
err := client.GenerateChatCompletionStream(ctx, "gpt-4o-mini", "Explain quantum computing", func(chunk string) error {
    fmt.Print(chunk) // Stream token
    return nil
})
```
*Note: If no API key is provided, the client falls back to simulated token streaming so live WebSocket UI animations function during hackathon prototyping.*

### 3. Vector Embeddings
```go
vec, err := client.GenerateEmbedding(ctx, "Text to embed")
// Returns []float32 with 1536 dimensions (L2 normalized)
```

---

## 11. Background Job Scheduling & Workers

### Job Scheduler (`internal/jobs/scheduler.go`)

A lightweight, concurrent scheduler designed to execute recurring background tasks without external queue brokers (like RabbitMQ or Redis Celery).

#### Features:
- **Interval Scheduling**: Runs tasks at custom intervals (e.g. every 30 seconds, every 10 minutes).
- **Per-Job Panic Isolation**: If a scheduled job encounters a runtime panic, the scheduler catches it via `recover()`, prints the full stack trace, logs the failure, and continues running other jobs without crashing the server.
- **Error Channel Reporting**: Execution failures are routed through an internal error channel for alerting.
- **Context-Aware Lifecycle**: Automatically stops when the application receives a shutdown signal, waiting for active jobs to complete.

#### Registering a Scheduled Job
```go
scheduler := jobs.NewScheduler()

// 1. Register a struct implementing jobs.Job
scheduler.RegisterInterval(jobs.NewHeartbeatJob("backend"), 30*time.Second)

// 2. Register an inline function
scheduler.RegisterFunc("cache-cleanup", 5*time.Minute, func(ctx context.Context) error {
    log.Info("Flushing stale cache items...")
    return nil
})

// Start with application context
scheduler.Start(ctx)
```

### Continuous Background Workers (`internal/worker/worker.go`)

For continuous tasks like queue consumers, billing calculations, or stream processors:

```go
worker := worker.NewBackgroundWorker("event-processor", 45*time.Second)
go worker.Run(ctx)
```

---

## 12. Health Checks & Observability

The boilerplate exposes Kubernetes and container probe endpoints:

### Liveness Probe (`GET /api/v1/health/live`)
Confirms the HTTP server process is running and accepting sockets. Always returns HTTP 200:
```json
{
  "success": true,
  "data": {
    "status": "alive",
    "time": "2026-09-22T05:45:00Z"
  }
}
```

### Readiness Probe (`GET /api/v1/health/ready`)
Checks whether all connected dependencies (e.g. PostgreSQL) are online and healthy.

- **Healthy (HTTP 200)**:
  ```json
  {
    "success": true,
    "data": {
      "status": "ready",
      "services": {
        "postgres": "up"
      },
      "time": "2026-09-22T05:45:00Z"
    }
  }
  ```
- **Degraded (HTTP 503)**: Returns HTTP 503 when any dependency ping fails:
  ```json
  {
    "success": false,
    "data": {
      "status": "degraded",
      "services": {
        "postgres": "down: dial tcp: connection refused"
      },
      "time": "2026-09-22T05:45:00Z"
    }
  }
  ```

#### Registering Custom Dependency Checkers
Implement the simple `handler.Checker` interface:
```go
type Checker interface {
    Name() string
    Check(ctx context.Context) error
}

healthHandler.RegisterChecker(myRedisClient)
```

---

## 13. Graceful Lifecycle & Shutdown

Implemented in `cmd/server/main.go`:

1. **Signal Notification**: Listens for `syscall.SIGINT` (Ctrl+C) and `syscall.SIGTERM` (Docker / Kubernetes termination).
2. **Orderly Teardown Sequence**:
   - Stops accepting new HTTP connections via `srv.Shutdown(shutdownCtx)`.
   - Waits for active HTTP requests to complete within `SHUTDOWN_TIMEOUT_SECONDS`.
   - Halts the background Job Scheduler (`scheduler.Stop()`) and waits for in-flight tasks.
   - Closes the PostgreSQL connection pool (`pgDB.Close()`).
   - Flushes buffered log entries via `log.Sync()`.

---

## 14. Docker & Containerization

### Multi-Stage `Dockerfile`
1. **Stage 1 (Builder)**: Uses `golang:1.24-alpine` to compile a statically linked Linux executable with compiler stripping (`-ldflags="-w -s"`).
2. **Stage 2 (Runtime)**: Minimal `alpine:3.21` container with `ca-certificates`, `tzdata`, a dedicated non-root user (`appuser`), and a built-in container `HEALTHCHECK`.

### Docker Compose (`docker-compose.yml`)
Spins up both the backend application and a PostgreSQL 16 Alpine database with health checks and persistent volume mapping:

```bash
# Start all services in background
docker compose up -d --build

# View real-time logs
docker compose logs -f

# Shutdown and clean containers
docker compose down
```

---

## 15. Recipes & Common Extensions

### Recipe 1: Adding a New Database Model & Store

1. Create `internal/store/pg_store/users.go`:
   ```go
   package pgstore

   type UserModel struct {
       ID    string `json:"id"`
       Email string `json:"email"`
   }

   func CreateUser(ctx context.Context, db *dbclient.PostgresDatabase, u *UserModel) error {
       _, err := db.Client.ExecContext(ctx, "INSERT INTO users (id, email) VALUES ($1, $2)", u.ID, u.Email)
       return err
   }
   ```
2. Call `CreateUser` from your handler.

### Recipe 2: Writing a New WebSocket Room Handler

1. Create a struct implementing `simplysocket.RoomData`:
   ```go
   package ws

   import "github.com/DhruvikDonga/simplysocket"

   type GameRoomHandler struct{}

   func (g *GameRoomHandler) HandleRoomData(room simplysocket.Room, server simplysocket.MeshServer) {
       for {
           select {
           case msg := <-room.ConsumeRoomMessage():
               if msg.Action == "move" {
                   room.BroadcastMessage(msg)
               }
           case <-room.RoomStopped():
               return
           }
       }
   }
   ```
2. Attach it to any room dynamically:
   ```go
   wsManager.JoinRoom("match_42", clientID, &GameRoomHandler{})
   ```

### Recipe 3: Generating and Testing a JWT Token

```go
import "hack-go-thon/internal/api/middleware"

token, err := middleware.GenerateToken(
    cfg.JWTSecret,
    "usr_abc123",
    "developer@example.com",
    "admin",
    24*time.Hour,
)

// Pass in HTTP header:
// Authorization: Bearer <token>
```

---

## 16. WebRTC Real-Time Media & Pion DataChannels

> 📖 **Comprehensive Deep Dive**: For an in-depth architectural breakdown of WebRTC SFU topologies, sequence diagrams, live WebSocket renegotiation queues (`renegotiatePending`), raw RTP packet forwarding, and RTCP PLI keyframe propagation, see [WEBRTC_INFO.md](WEBRTC_INFO.md).

The boilerplate provides comprehensive WebRTC support for mobile apps (iOS / Android / Flutter) and web clients, combining **simplysocket P2P signaling** with **Pion WebRTC server-side peer sessions**.

### 16.1 Architecture Overview

1. **Signaling Hub (`internal/ws/webrtc_handler.go`)**:
   Peers join rooms (`call-<name>` or `webrtc-<name>`) over `/api/v1/ws` and exchange SDP offers, answers, and ICE candidates using `simplysocket`.
2. **ICE Configuration Endpoint (`GET /api/v1/webrtc/ice-servers`)**:
   Provides standard `RTCIceServer` STUN/TURN configurations with environment variable overrides.
3. **Pion WebRTC Server Peer (`internal/webrtc_server/server.go`)**:
   Terminates client-to-server WebRTC sessions via `POST /api/v1/webrtc/server/session`, opening an ultra-low latency UDP `RTCDataChannel` for bi-directional messaging, telemetry, and ping-pong latency benchmarks.

### 16.2 Signaling Wire Protocol

| Action | Sender | Target | Description |
| :--- | :--- | :--- | :--- |
| `webrtc-join` | Client | Room | Announces presence in call room |
| `webrtc-peers` | Server | Room | Returns list of active peers in the room |
| `webrtc-peer-joined` | Server | Room | Broadcasts newly joined peer ID to other occupants |
| `webrtc-offer` | Client | Target Peer | Forwards SDP offer (`{"target_id": "...", "sdp": {...}}`) |
| `webrtc-answer` | Client | Target Peer | Forwards SDP answer (`{"target_id": "...", "sdp": {...}}`) |
| `webrtc-ice` | Client | Target Peer | Exchanges ICE candidate network descriptors |
| `webrtc-leave` | Client | Room | Gracefully leaves call session |

### 16.3 Client-to-Server Pion UDP DataChannel

To establish an ultra-low latency UDP connection directly with the Go backend:
```bash
# Client creates offer and sends to backend:
curl -X POST http://localhost:8080/api/v1/webrtc/server/session \
  -H "Content-Type: application/json" \
  -d '{"sdp": "..."}'
```
Response:
```json
{
  "success": true,
  "data": {
    "session_id": "8f2d5a31-...",
    "answer": {
      "type": "answer",
      "sdp": "..."
    }
  }
}
```
Once the answer is set as the remote description, the `RTCDataChannel` opens instantly over UDP. Clients can send `ping:<timestamp>` to receive an immediate `pong:<timestamp>` response for real-time latency measurement.

### 16.4 Pion SFU (Selective Forwarding Unit) Multi-Party Conferencing

When scaling beyond 2 participants, P2P mesh requires $N-1$ uplinks per peer, quickly exhausting mobile upload bandwidth. The built-in Pion SFU acts as a media router with **1 uplink per client** and low-latency raw RTP forwarding:

```
[Mobile Peer 1] --(1 Uplink RTP Track)--> [ Go Backend Pion SFU ] --(RTP Fan-Out)--> [Mobile Peer 2]
                                                                  --(RTP Fan-Out)--> [Web Dashboard]
```

#### SFU REST Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/webrtc/sfu/join` | Ingests client SDP offer with local media tracks, attaches room tracks, returns SDP answer |
| `POST` | `/api/v1/webrtc/sfu/renegotiate` | Updates peer connection when new participants publish tracks in the room |
| `POST` | `/api/v1/webrtc/sfu/leave` | Disconnects peer, removes published tracks, and cleans up empty rooms |
| `DELETE`| `/api/v1/webrtc/sfu/rooms/:room_id/peers/:peer_id` | RESTful peer disconnection alternative |
| `GET`  | `/api/v1/webrtc/sfu/rooms` | Lists active SFU rooms, peer count, participant slugs, and ingested tracks |

#### Periodic RTCP Keyframe Recovery
To ensure subscribers receive instantaneous video upon joining an already-streaming room, the SFU runs a dedicated RTCP routine emitting `rtcp.PictureLossIndication` (PLI) packets to publishers every 3 seconds, triggering video encoders to emit instantaneous I-frames.

---

## 17. Mobile Webhook Notification System

The built-in webhook subsystem enables mobile teams and third-party services to subscribe to real-time backend events (system announcements, alerts, chat messages, user updates) and receive reliable, signed HTTP POST notifications.

### 17.1 Webhook Wire Protocol & Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/webhooks` | Register a new mobile webhook subscription |
| `GET` | `/api/v1/webhooks` | List all active webhook endpoints |
| `GET` | `/api/v1/webhooks/:id` | Retrieve subscription details by ID |
| `PUT` | `/api/v1/webhooks/:id` | Update target URL, event filters, secret, or active state |
| `DELETE`| `/api/v1/webhooks/:id` | Remove a webhook subscription |
| `POST` | `/api/v1/webhooks/send` | Dispatch a notification payload to all matching webhooks & WebSocket mesh |
| `POST` | `/api/v1/webhooks/test` | Execute an on-demand test ping and report immediate latency and HTTP status |
| `GET` | `/api/v1/webhooks/logs` | View recent delivery history, duration, and status codes |

### 17.2 Registering a Mobile Webhook
```bash
curl -X POST http://localhost:8080/api/v1/webhooks \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://push-gateway.my-app.com/v1/notifications",
    "events": ["notification", "system_alert"],
    "secret": "my-mobile-hmac-secret-key",
    "description": "Mobile Push Notification Relay"
  }'
```
Response:
```json
{
  "success": true,
  "data": {
    "id": "wh_1727245800_a1b2c3d4",
    "url": "https://push-gateway.my-app.com/v1/notifications",
    "events": ["notification", "system_alert"],
    "description": "Mobile Push Notification Relay",
    "active": true,
    "created_at": "2026-09-25T11:50:00Z"
  }
}
```

### 17.3 HMAC-SHA256 Payload Signature Verification
When a webhook specifies a `secret`, outgoing HTTP requests include cryptographic authentication headers:
- `X-Webhook-Signature`: `sha256=<hex_hmac>` computed over the raw JSON body using the shared secret.
- `X-Webhook-Event`: The event category (e.g. `notification`, `chat_message`).
- `X-Webhook-Delivery`: Unique delivery trace ID.
- `X-Webhook-Timestamp`: RFC3339 dispatch timestamp.

Verifying the signature in Go:
```go
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(rawBody)
expectedSig := hex.EncodeToString(mac.Sum(nil))
isValid := hmac.Equal([]byte(receivedSig), []byte(expectedSig))
```

### 17.4 Dispatching Notifications & WebSocket Synchronization
```bash
curl -X POST http://localhost:8080/api/v1/webhooks/send \
  -H "Content-Type: application/json" \
  -d '{
    "event": "notification",
    "title": "Maintenance Alert",
    "message": "Scheduled maintenance starting in 15 minutes.",
    "target": "mobile_group_all",
    "priority": "high",
    "data": { "deep_link": "/settings/maintenance" }
  }'
```
The dispatch pipeline:
1. Filters active subscriptions matching the event topic.
2. Dispatches asynchronous HTTP POST requests in parallel with an isolated 5-second timeout.
3. Automatically broadcasts `action: "webhook-notification"` over the `simplysocket` mesh to the dedicated `"webhooks"` room (rather than flooding `mesh-global`), notifying real-time worker nodes and staff dashboards.

### 17.5 Dedicated 'webhooks' WebSocket Room & Access Control

Rather than broadcasting webhook job events across the public `mesh-global` channel, notifications are scoped strictly to the dedicated **`webhooks`** room (alias `webhook-jobs`):

* **Access Restriction (Excludes User Level)**: Standard user-level accounts with **Auth Level $\le 1$** (such as regular mobile app users, demo members, and default registered users) are denied entry to the `webhooks` room.
* **Allowed Accounts**: All roles above user level (**Auth Level $> 1$**, Levels 2 to 99 — including Staff Level 10, Moderators, Superadmins Level 99, and background worker services) are authorized to join the `webhooks` room.
* **Joining via WebSocket**:
  ```json
  {
    "action": "join-room",
    "target": "mesh-global",
    "message_body": {
      "room": "webhooks",
      "token": "<staff_or_admin_jwt>"
    }
  }
  ```
  If an unauthorized user-level client attempts to join, the server rejects the request with `joined-room-ack` error status: `"Unauthorized: webhooks room excludes user-level accounts (requires Auth Level > 1, e.g. staff or admin)"`.

### 17.6 PostgreSQL Database Persistence & Delivery Logs

When PostgreSQL is connected (`SERVICES_DATABASE=true`), webhooks and delivery metrics are persisted directly into PostgreSQL tables:

#### Database Schema
* **`webhook_subscriptions`**:
  * `id VARCHAR(64) PRIMARY KEY`: Unique identifier (`wh_<timestamp>_<hex>`).
  * `url TEXT NOT NULL`: Target HTTP/HTTPS endpoint.
  * `events JSONB NOT NULL`: Subscribed topic filters (e.g. `["notification", "system_alert"]` or `["*"]`).
  * `secret TEXT`: Shared secret used for HMAC-SHA256 request signatures.
  * `description TEXT`: Purpose or subscriber application label.
  * `active BOOLEAN`: Enabled / disabled state (indexed via `idx_webhook_subscriptions_active`).
  * `created_at TIMESTAMPTZ`, `updated_at TIMESTAMPTZ`: Auto-managed timestamps.
* **`webhook_delivery_logs`**:
  * `id VARCHAR(64) PRIMARY KEY`: Unique delivery attempt trace ID.
  * `webhook_id VARCHAR(64)`: Associated subscription ID.
  * `url TEXT`, `event VARCHAR(100)`: Target and event topic.
  * `status_code INT`, `duration_ms BIGINT`: HTTP response metrics.
  * `success BOOLEAN`, `error TEXT`: Status code `< 300` flag and error detail.
  * `payload_preview TEXT`: Serialized JSON payload snapshot.
  * `created_at TIMESTAMPTZ`: Indexed reverse-chronologically for real-time audit queries (`idx_webhook_logs_created_at`).

#### Resilient Dual-Mode Operation
* **Database Mode**: On startup, `pgstore.InitWebhookSchema(ctx, pgDB)` auto-migrates the tables and indexes. Subscriptions are loaded into memory for zero-overhead asynchronous dispatching, and all CRUD changes and delivery attempts are synchronized with PostgreSQL.
* **Offline / Demo Mode**: If PostgreSQL is disabled (`SERVICES_DATABASE=false`) or unreachable, `WebhookHandler` gracefully switches to thread-safe in-memory maps and ring buffers, ensuring zero downtime and fully working local unit tests.

---

## 18. Multipart Form File Upload API

The file upload subsystem provides standard HTTP `multipart/form-data` processing for mobile applications uploading user avatars, voice recordings, media attachments, and documents.

### 18.1 Upload Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/upload` | Ingest `multipart/form-data` containing `file` or `files` |
| `GET` | `/api/v1/files` | Catalog listing of uploaded assets with metadata |
| `GET` | `/api/v1/files/:filename` | Stream or view file with MIME type headers |
| `GET` | `/api/v1/files/:filename?download=true` | Download file with attachment `Content-Disposition` |
| `DELETE`| `/api/v1/files/:filename` | Safely remove file from storage and catalog |

### 18.2 Uploading Files via Multipart Form
Mobile clients upload files using standard HTTP multipart form data:

```bash
curl -X POST http://localhost:8080/api/v1/upload \
  -F "file=@avatar.png" \
  -F "category=avatars" \
  -F "description=User profile photo"
```

Response:
```json
{
  "success": true,
  "data": {
    "id": "file_1727245800_f4a1",
    "original_name": "avatar.png",
    "stored_name": "1727245800_f4a1_avatar.png",
    "size_bytes": 1048576,
    "size_formatted": "1.00 MB",
    "mime_type": "image/png",
    "category": "avatars",
    "description": "User profile photo",
    "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "url": "/api/v1/files/1727245800_f4a1_avatar.png",
    "download_url": "/api/v1/files/1727245800_f4a1_avatar.png?download=true",
    "uploaded_at": "2026-09-25T11:55:00Z"
  }
}
```

Multiple files can be uploaded concurrently in a single multipart request by providing multiple `files` fields.

### 18.3 Security & Safety Invariants
1. **Path Traversal Protection**: All filenames are passed through `filepath.Base` and strictly sanitized to prevent directory traversal (`../`).
2. **Magic Byte MIME Sniffing**: Inspects the first 512 bytes via `http.DetectContentType` before falling back to extension matching, blocking extension-spoofing attacks.
3. **Configurable File Size Limits**: Enforced via `MAX_UPLOAD_SIZE_MB` (default `32 MB`). Payloads exceeding this threshold are immediately rejected with `400 Bad Request`.
4. **Storage Isolation**: Assets are stored in `UPLOAD_DIR` (default `./uploads`) with directory creation permissions `0755`.




