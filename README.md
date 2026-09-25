# Hack-Go-Thon 🚀

A production-grade, modular Go backend boilerplate designed for rapid hackathon velocity and robust microservice development. It provides everything needed to build, demo, and ship high-performance real-time and AI-powered applications out of the box.

> 📖 **Full Technical Manual**: For comprehensive architecture diagrams, directory structure, detailed code walkthroughs, API contracts, and recipes, see [**`DOCUMENTATION.md`**](DOCUMENTATION.md).

---

## ✨ Features

- 🌐 **Real-Time WebSockets via [`simplysocket`](https://github.com/DhruvikDonga/simplysocket)**  
  High-concurrency mesh architecture based on *"Connect once, write logic multiple times"* by [Dhruvik](https://github.com/DhruvikDonga). Clients connect via a single WebSocket endpoint (`/api/v1/ws`) while business features are implemented independently through pluggable `RoomData` handlers.

- 🤖 **Real-Time LLM Token Streaming**  
  Stream model completion tokens piece-by-piece over WebSockets directly to clients using [`simplysocket`](https://github.com/DhruvikDonga/simplysocket). Includes automatic zero-dependency simulated streaming when offline or without an API key so demo velocity is never blocked.

- 🗄️ **PostgreSQL Vector DB & RAG Pipeline (`pgvector`)**  
  Native in-database vector storage using `pgvector/pgvector:pg16`. Features 1536-dimensional embeddings, an HNSW cosine similarity index (`<=>`), and ready-to-use endpoints for document indexing (`/api/v1/rag/documents`), semantic search (`/api/v1/rag/search`), and grounded Q&A (`/api/v1/rag/ask`).

- 🖥️ **Embedded Admin Control Center UI**  
  A sleek, responsive dark-mode dashboard bundled directly into the Go binary (`web/admin.html`) and served at `/admin` (and `/`). Provides live mesh room/client visualization, real-time activity feed, live LLM streaming tester, scheduled jobs monitor, an interactive RAG playground, and a live WebRTC Lab for P2P video calls and UDP DataChannels.
![alt text](image-1.png)
![alt text](image.png)

- 📹 **WebRTC Pion SFU & 1:1 Real-Time Media Hub** *(see detailed architecture & sequence diagrams in [`WEBRTC_INFO.md`](WEBRTC_INFO.md))*  
  Enterprise-grade WebRTC subsystem powered by [`pion/webrtc/v4`](https://github.com/pion/webrtc):
  - **Selective Forwarding Unit (SFU)**: $O(1)$ mobile uplink bandwidth routing raw RTP video and audio streams across multi-party rooms (`/api/v1/webrtc/sfu/*`), featuring live WebSocket track synchronization, header extension stripping, and 3-second RTCP PLI keyframe heartbeats.
  - **1:1 P2P Audio/Video**: Direct mesh signaling over [`simplysocket`](https://github.com/DhruvikDonga/simplysocket).
  - **Sub-Millisecond UDP DataChannel**: Server-managed Pion peer connections for raw UDP messaging and ping-pong latency benchmarking.
  - **ICE & NAT Traversal**: Automated STUN/TURN configuration (`/api/v1/webrtc/ice-servers`).

- 🎛️ **Modular Subsystem Feature Flags (`services.json`)**  
  Selectively initialize only the services your hackathon or production workload requires (`services.json` or `SERVICES_CONFIG_PATH`). All components default to `true` with support for lenient aliases (`db`, `ws`, `api`, `sfu`, `scheduler`):
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
  Disabling a flag cleanly omits database connections, background goroutines, and routes with zero code changes.

- ⚙️ **In-Process Job Scheduler & Workers**  
  Periodic task scheduler (`jobs.Scheduler`) with panic isolation, runtime recovery, and live execution observability (`GET /api/v1/jobs`), plus continuous background worker loops without external broker dependencies.

- 👥 **Pre-Setup User Base & Multi-Level RBAC Auth**  
  Production-ready user authentication with `username`, `phone_number`, `email`, bcrypt password hashing, and custom `JSONB` metadata embedding `auth_level`. Directly embeds user metadata into JWT claims to support multi-level authorization/RBAC out of the box without complex 3rd-party auth services (Firebase, Auth0). Includes `RequireAuthLevel(minLevel)` middleware, zero-dependency in-memory fallback mode for offline demos, and a 1-click token inspector in the Admin Control Center:
  - **Pre-Seeded Accounts**:
    - **Superadmin (Level 99)**: `admin@hack-go-thon.local` / `Mp@tel98` (phone `9427425572`) — full read/write, user management, and `admin` room access.
    - **Staff Viewer (Level 10)**: `staff@hack-go-thon.local` / `Staff@123` (phone `9427425570`) — view-only dashboard access. Restricted from joining room `"admin"` and modifying (creating/updating/deleting) users.
    - **Demo Member (Level 1)**: `user@hackathon.local` / `user123` — standard member privileges.
  - **simplysocket Room-Level Security & Direct Broadcast**: The WebSocket connection endpoint (`/api/v1/ws`) is open to all clients. All connected users join `mesh-global` and can broadcast directly using `action: "broadcast"`. Admission to the special `"admin"` room strictly requires Auth Level $> 10$ (Level 11–99), where admins can broadcast directly to room `"admin"`.

- 🔔 **Mobile Webhook Notification Subsystem (`/api/v1/webhooks/*`)**  
  Built for mobile app push relays and third-party event consumers. Allows registering webhook URLs with topic filtering (`events: ["*"]` or `["notification", "alert"]`), cryptographic payload signing (`X-Webhook-Signature: sha256=<hmac>`), non-blocking asynchronous dispatch, live ping testing (`POST /api/v1/webhooks/test`), delivery logs, and automatic synchronization across the `simplysocket` WebSocket mesh.

- 📁 **Multipart Form File Upload API (`/api/v1/upload` & `/api/v1/files/*`)**  
  Production-ready file upload engine ingesting `multipart/form-data` for single or multiple files (images, audio notes, media attachments, documents). Features path traversal protection, magic-byte MIME type sniffing (`http.DetectContentType`), configurable size limits (`MAX_UPLOAD_SIZE_MB`), SHA256 integrity checksums, and streaming or attachment download serving (`GET /api/v1/files/:filename?download=true`).

- 🔐 **Authentication & Security Middlewares**  
  Production-ready authentication middleware pipeline with JWT (`Bearer <token>`) validation/generation, custom claims RBAC verification, and database-backed API Key (`X-API-Key`) checking with master key bypass.

- 📊 **Audit Logging & Structured Telemetry**  
  Asynchronous PostgreSQL request audit logging (`api_calls`), high-performance Zap structured logging with console/JSON modes, and unique `X-Request-ID` correlation.

- 🩺 **Kubernetes Health Probes & Graceful Shutdown**  
  Standardized `/api/v1/health/live` and `/api/v1/health/ready` probes with dependency checking, paired with clean signal handling (`SIGINT`/`SIGTERM`) and context-aware connection draining.

- 🐳 **Docker & Container Ready**  
  Multi-stage minimal Alpine `Dockerfile` and local `docker-compose.yml` pre-configured with `pgvector/pgvector:pg16` and health check dependencies.

---

## ⚡ Quick Start

```bash
# Run locally (starts on http://localhost:8080)
make run

# Or spin up with PostgreSQL (pgvector) in Docker
make docker-up
```

Open **[http://localhost:8080/admin](http://localhost:8080/admin)** to access the live Control Center!
- **Default Superadmin Login**: `admin@hack-go-thon.local` / `Mp@tel98` (phone `9427425572`, Level 99) via the **"Fill Admin (Lvl 99)"** button.
- **Default Staff Viewer Login**: `staff@hack-go-thon.local` / `Staff@123` (phone `9427425570`, Level 10 View-Only) via the **"Fill Staff (Lvl 10)"** button.

---

## 🤖 Agent Development & Skills

* 📖 **[SKILLS.md](./SKILLS.md)**: Authoritative architecture patterns, invariant safety rules, and step-by-step recipes for AI coding agents.
* 📚 **[DOCUMENTATION.md](./DOCUMENTATION.md)**: Deep-dive architecture reference and API schemas.

## 👤 Author

By [Dhruvik](https://dhruvik.cc)

