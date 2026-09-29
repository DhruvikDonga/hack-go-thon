package api

import (
	"hack-go-thon/config"
	"hack-go-thon/internal/api/handler"
	"hack-go-thon/internal/api/middleware"
	dbclient "hack-go-thon/internal/db_client"
	"hack-go-thon/internal/jobs"
	"hack-go-thon/internal/ws"
	"hack-go-thon/pkg/response"
	"hack-go-thon/web"
	"sort"

	"github.com/DhruvikDonga/simplysocket"
	"github.com/gin-gonic/gin"
)

// RouterConfig contains dependencies for building the HTTP router.
type RouterConfig struct {
	Config           *config.Config
	HealthHandler    *handler.HealthHandler
	ExampleHandler   *handler.ExampleHandler
	RAGHandler       *handler.RAGHandler
	WebRTCHandler    *handler.WebRTCHandler
	UserHandler      *handler.UserHandler
	WebhookHandler   *handler.WebhookHandler
	UploadHandler    *handler.UploadHandler
	TelemetryHandler *handler.TelemetryHandler
	DB               *dbclient.PostgresDatabase
	WSManager        *ws.Manager
	Scheduler        *jobs.Scheduler
}

// SetupRouter initializes the Gin engine with all global middlewares and routes.
func SetupRouter(rc RouterConfig) *gin.Engine {
	switch rc.Config.Environment {
	case "production", "release":
		gin.SetMode(gin.ReleaseMode)
	case "test":
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}

	engine := gin.New()

	// Global Middlewares
	engine.Use(middleware.CORS(rc.Config.AllowedOrigins))
	engine.Use(middleware.APILogger())
	engine.Use(middleware.Recovery())

	// Audit API Logger Middleware (writes to PostgreSQL if active)
	if rc.DB != nil {
		engine.Use(middleware.APICallLogger(rc.DB))
	}

	// Handle preflight OPTIONS
	engine.OPTIONS("/*path", func(c *gin.Context) {
		c.AbortWithStatus(204)
	})

	// Embedded Admin Control Center UI Dashboard (served at /admin and /)
	serveAdminUI := func(c *gin.Context) {
		c.Data(200, "text/html; charset=utf-8", web.AdminHTML)
	}
	engine.GET("/admin", serveAdminUI)
	engine.GET("/", serveAdminUI)

	// API v1 Route Group
	v1 := engine.Group("/api/v1")
	{
		// @Summary Get all router APIs catalog
		// @Description Returns dynamic JSON catalog of all router endpoints generated from router comments
		// @Tags Admin
		// @Auth open
		v1.GET("/admin/apis", func(c *gin.Context) {
			c.Data(200, "application/json; charset=utf-8", web.AdminAPIsJSON)
		})

		// Health Probes
		health := v1.Group("/health")
		{
			// @Summary Check service liveness
			// @Description Returns 200 OK if server process is running
			// @Tags Health
			// @Auth open
			health.GET("/live", rc.HealthHandler.Live)

			// @Summary Check service readiness
			// @Description Checks if database and dependent components are ready
			// @Tags Health
			// @Auth open
			health.GET("/ready", rc.HealthHandler.Ready)
		}

		// WebSocket Endpoint (Single connection point for multiple RoomData logic implementations)
		if rc.WSManager != nil {
			// @Summary WebSocket Mesh Gateway
			// @Description Connect to real-time simplysocket mesh with client name and token
			// @Tags WebSocket
			// @Auth open
			// @Param name query string false "Client name"
			// @Param token query string false "JWT Token"
			v1.GET("/ws", rc.WSManager.Handler())

			// @Summary List active mesh rooms & clients
			// @Description Telemetry endpoint returning all simplysocket rooms, client counts, and client slugs
			// @Tags WebSocket
			// @Auth open
			v1.GET("/ws/rooms", func(c *gin.Context) {
				roomsMap := rc.WSManager.Server().GetClientsInRoom()
				allRooms := rc.WSManager.Server().GetRooms()
				allClients := rc.WSManager.Server().GetClients()

				roomSet := make(map[string]bool)
				for r := range roomsMap {
					roomSet[r] = true
				}
				for _, r := range allRooms {
					roomSet[r] = true
				}
				roomSet[simplysocket.MeshGlobalRoom] = true

				type roomInfo struct {
					Name        string   `json:"name"`
					ClientCount int      `json:"client_count"`
					Clients     []string `json:"clients"`
				}

				var roomsList []roomInfo
				uniqueClients := make(map[string]bool)

				for rName := range roomSet {
					var slugs []string
					if cmap, ok := roomsMap[rName]; ok {
						for slug := range cmap {
							if slug != "" {
								slugs = append(slugs, slug)
								uniqueClients[slug] = true
							}
						}
					}
					sort.Strings(slugs)
					roomsList = append(roomsList, roomInfo{
						Name:        rName,
						ClientCount: len(slugs),
						Clients:     slugs,
					})
				}
				for slug := range allClients {
					if slug != "" {
						uniqueClients[slug] = true
					}
				}
				sort.Slice(roomsList, func(i, j int) bool {
					return roomsList[i].Name < roomsList[j].Name
				})

				response.OK(c, gin.H{
					"total_rooms":   len(roomsList),
					"total_clients": len(uniqueClients),
					"rooms":         roomsList,
				})
			})
		}

		// Public Example Resource Endpoints
		if rc.ExampleHandler != nil {
			items := v1.Group("/items")
			{
				// @Summary Create sample item
				// @Description Ingests and stores a new sample item
				// @Tags Items
				// @Auth open
				// @Body {"name": "Example Item", "description": "Sample description"}
				items.POST("", rc.ExampleHandler.CreateItem)

				// @Summary List all items
				// @Description Returns array of all sample items
				// @Tags Items
				// @Auth open
				items.GET("", rc.ExampleHandler.ListItems)

				// @Summary Get item by ID
				// @Description Returns a single item by its unique identifier
				// @Tags Items
				// @Auth open
				// @Param id path string true "Item ID"
				items.GET("/:id", rc.ExampleHandler.GetItem)
			}

			// LLM Endpoints
			llm := v1.Group("/llm")
			{
				// @Summary Ask AI completion
				// @Description Generates completion from OpenAI models
				// @Tags LLM
				// @Auth open
				// @Body {"prompt": "What is WebRTC?", "model": "gpt-4o-mini"}
				llm.POST("/ask", rc.ExampleHandler.AskAI)
			}
		}

		// Postgres pgvector RAG Endpoints
		if rc.RAGHandler != nil {
			rag := v1.Group("/rag")
			{
				// @Summary Index RAG document
				// @Description Computes 1536-d embedding vector and persists document in PostgreSQL pgvector
				// @Tags RAG
				// @Auth open
				// @Body {"title": "Architecture Overview", "content": "Microservices built with Go and PostgreSQL"}
				rag.POST("/documents", rc.RAGHandler.CreateDocument)

				// @Summary List indexed RAG documents
				// @Description Retrieves all vectorized documents from pgvector store
				// @Tags RAG
				// @Auth open
				rag.GET("/documents", rc.RAGHandler.ListDocuments)

				// @Summary KNN semantic vector search
				// @Description Performs cosine distance (<=>) KNN search in pgvector
				// @Tags RAG
				// @Auth open
				// @Body {"query": "What database is used?", "limit": 5}
				rag.POST("/search", rc.RAGHandler.SearchDocuments)

				// @Summary Ask question with RAG context
				// @Description Injects pgvector search context into prompt and generates grounded LLM answer
				// @Tags RAG
				// @Auth open
				// @Body {"question": "How do we store vector embeddings?"}
				rag.POST("/ask", rc.RAGHandler.AskRAG)
			}
		}

		// Scheduled Background Jobs Endpoint
		if rc.Scheduler != nil {
			// @Summary List background cron jobs
			// @Description Returns scheduled tasks, cron intervals, execution states, and latencies
			// @Tags Jobs
			// @Auth open
			v1.GET("/jobs", func(c *gin.Context) {
				response.OK(c, rc.Scheduler.GetTasks())
			})
		}

		// WebRTC Signaling & Configuration Endpoints
		if rc.WebRTCHandler != nil {
			webrtcGroup := v1.Group("/webrtc")
			{
				// @Summary Get ICE STUN servers
				// @Description Returns configured STUN/TURN servers for WebRTC peer connection ICE negotiation
				// @Tags WebRTC
				// @Auth open
				webrtcGroup.GET("/ice-servers", rc.WebRTCHandler.GetICEServers)

				// @Summary WebRTC subsystem status
				// @Description Checks WebRTC Pion engine status and active sessions
				// @Tags WebRTC
				// @Auth open
				webrtcGroup.GET("/status", rc.WebRTCHandler.GetStatus)

				// @Summary Create server-side WebRTC session
				// @Description Initiates Pion WebRTC PeerConnection session over SDP
				// @Tags WebRTC
				// @Auth open
				// @Body {"sdp": "v=0\r\no=- 0 0 IN IP4 127.0.0.1..."}
				webrtcGroup.POST("/server/session", rc.WebRTCHandler.CreateServerSession)

				// @Summary Close server-side WebRTC session
				// @Description Terminates an active Pion server session
				// @Tags WebRTC
				// @Auth open
				// @Param id path string true "Session ID"
				webrtcGroup.DELETE("/server/session/:id", rc.WebRTCHandler.CloseServerSession)

				// SFU Multi-Party Conference Endpoints
				sfu := webrtcGroup.Group("/sfu")
				{
					// @Summary WebRTC SFU WebSocket signaling
					// @Description Upgrades connection for SFU conference signaling
					// @Tags WebRTC
					// @Auth open
					sfu.GET("/ws", rc.WebRTCHandler.HandleSFUWS)

					// @Summary Join SFU conference room
					// @Description Joins Pion SFU room with publisher/subscriber tracks
					// @Tags WebRTC
					// @Auth open
					// @Body {"room_id": "conf-alpha", "peer_id": "peer-1", "sdp": "..."}
					sfu.POST("/join", rc.WebRTCHandler.JoinSFU)

					// @Summary Renegotiate SFU tracks
					// @Description Updates active tracks and renegotiates SDP offer/answer
					// @Tags WebRTC
					// @Auth open
					// @Body {"room_id": "conf-alpha", "peer_id": "peer-1", "sdp": "..."}
					sfu.POST("/renegotiate", rc.WebRTCHandler.RenegotiateSFU)

					// @Summary Leave SFU room
					// @Description Disconnects peer from SFU room and closes RTP tracks
					// @Tags WebRTC
					// @Auth open
					// @Body {"room_id": "conf-alpha", "peer_id": "peer-1"}
					sfu.POST("/leave", rc.WebRTCHandler.LeaveSFU)

					// @Summary Remove peer from SFU room
					// @Description Kicks peer and terminates tracks in SFU room
					// @Tags WebRTC
					// @Auth open
					// @Param room_id path string true "Room ID"
					// @Param peer_id path string true "Peer ID"
					sfu.DELETE("/rooms/:room_id/peers/:peer_id", rc.WebRTCHandler.LeaveSFU)

					// @Summary List active SFU conference rooms
					// @Description Returns all active Pion SFU rooms, peer lists, and track counts
					// @Tags WebRTC
					// @Auth open
					sfu.GET("/rooms", rc.WebRTCHandler.GetSFURooms)
				}
			}
		}

		// User & Authentication Endpoints (Multi-Level Auth & Metadata)
		if rc.UserHandler != nil {
			auth := v1.Group("/auth")
			{
				// @Summary Register new user account
				// @Description Creates a new account with hashed password and metadata
				// @Tags Users
				// @Auth open
				// @Body {"username": "alice", "email": "alice@example.com", "password": "Password123!", "role": "member", "auth_level": 1}
				auth.POST("/register", rc.UserHandler.Register)

				// @Summary Authenticate user & issue JWT
				// @Description Validates credentials and returns JWT token with auth_level claims
				// @Tags Users
				// @Auth open
				// @Body {"username": "admin", "password": "Password123!"}
				auth.POST("/login", rc.UserHandler.Login)

				// @Summary Get currently authenticated profile
				// @Description Validates Bearer JWT claims and returns current user info
				// @Tags Users
				// @Auth token
				// @Level 1
				auth.GET("/me", middleware.JWTAuth(rc.Config.JWTSecret), rc.UserHandler.Me)
			}

			users := v1.Group("/users")
			{
				// @Summary List registered users
				// @Description Returns list of all user accounts and JSONB metadata
				// @Tags Users
				// @Auth open
				users.GET("", rc.UserHandler.ListUsers)

				// @Summary Create user account
				// @Description Directly provisions a new user with role and metadata
				// @Tags Users
				// @Auth open
				// @Body {"username": "bob", "email": "bob@example.com", "password": "Password123!", "role": "moderator", "auth_level": 50}
				users.POST("", rc.UserHandler.CreateUser)

				// @Summary Get user by ID
				// @Description Returns single user account details by ID
				// @Tags Users
				// @Auth open
				// @Param id path string true "User ID"
				users.GET("/:id", rc.UserHandler.GetUser)

				// @Summary Update user account
				// @Description Updates user fields and JSONB metadata
				// @Tags Users
				// @Auth open
				// @Param id path string true "User ID"
				// @Body {"email": "updated@example.com", "role": "admin", "auth_level": 90}
				users.PUT("/:id", rc.UserHandler.UpdateUser)

				// @Summary Delete user account
				// @Description Permanently removes a user account
				// @Tags Users
				// @Auth open
				// @Param id path string true "User ID"
				users.DELETE("/:id", rc.UserHandler.DeleteUser)

				// @Summary Generate JWT token for user
				// @Description Issues a signed JWT with user's auth_level and metadata claims
				// @Tags Users
				// @Auth open
				// @Param id path string true "User ID"
				users.POST("/:id/token", rc.UserHandler.GenerateUserToken)
			}
		}

		// JWT Protected Routes Example (Multi-level verification)
		jwtProtected := v1.Group("/protected")
		jwtProtected.Use(middleware.JWTAuth(rc.Config.JWTSecret))
		{
			// @Summary Authenticated user profile probe
			// @Description Requires valid Bearer JWT token in Authorization header
			// @Tags Protected
			// @Auth token
			// @Level 1
			jwtProtected.GET("/profile", func(c *gin.Context) {
				response.OK(c, gin.H{
					"message":      "Access granted via valid JWT",
					"user_id":      c.GetString("user_id"),
					"username":     c.GetString("username"),
					"email":        c.GetString("email"),
					"phone_number": c.GetString("phone_number"),
					"role":         c.GetString("role"),
					"auth_level":   c.MustGet("auth_level"),
					"metadata":     c.MustGet("metadata"),
				})
			})

			// @Summary Admin-only route probe
			// @Description Requires Bearer JWT token with auth_level >= 50
			// @Tags Protected
			// @Auth token
			// @Level 50
			jwtProtected.GET("/admin-only", middleware.RequireAuthLevel(50), func(c *gin.Context) {
				response.OK(c, gin.H{
					"message":    "Access granted: Auth level >= 50 verified",
					"user_id":    c.GetString("user_id"),
					"username":   c.GetString("username"),
					"auth_level": c.MustGet("auth_level"),
					"metadata":   c.MustGet("metadata"),
				})
			})
		}

		// Webhook Notification Endpoints (Mobile Push & Integrations)
		if rc.WebhookHandler != nil {
			webhooks := v1.Group("/webhooks")
			{
				// @Summary Register mobile webhook gateway
				// @Description Registers an HTTP callback URL with event filters and optional HMAC secret
				// @Tags Webhooks
				// @Auth open
				// @Body {"url": "https://example.com/webhook", "events": ["*"], "secret": "my-secret-key", "description": "Mobile push"}
				webhooks.POST("", rc.WebhookHandler.Register)

				// @Summary List registered webhooks
				// @Description Returns all active webhook configurations
				// @Tags Webhooks
				// @Auth open
				webhooks.GET("", rc.WebhookHandler.List)

				// @Summary Get webhook by ID
				// @Description Returns single webhook configuration
				// @Tags Webhooks
				// @Auth open
				// @Param id path string true "Webhook ID"
				webhooks.GET("/:id", rc.WebhookHandler.Get)

				// @Summary Update webhook configuration
				// @Description Updates target URL, event subscriptions, or secret
				// @Tags Webhooks
				// @Auth open
				// @Param id path string true "Webhook ID"
				// @Body {"url": "https://example.com/new-webhook", "events": ["notification", "alert"]}
				webhooks.PUT("/:id", rc.WebhookHandler.Update)

				// @Summary Delete webhook subscription
				// @Description Removes a registered webhook
				// @Tags Webhooks
				// @Auth open
				// @Param id path string true "Webhook ID"
				webhooks.DELETE("/:id", rc.WebhookHandler.Delete)

				// @Summary Dispatch webhook notification
				// @Description Broadcasts notification event to matched webhooks and WebSocket mesh room
				// @Tags Webhooks
				// @Auth open
				// @Body {"event": "notification", "title": "System Alert", "message": "Deployment succeeded"}
				webhooks.POST("/send", rc.WebhookHandler.Send)

				// @Summary Test ping webhook
				// @Description Sends test ping request to target webhook and reports HTTP status and latency
				// @Tags Webhooks
				// @Auth open
				// @Body {"webhook_id": "optional-id", "url": "https://httpbin.org/post"}
				webhooks.POST("/test", rc.WebhookHandler.Test)

				// @Summary Webhook delivery audit logs
				// @Description Returns recent webhook delivery attempts, latencies, and response codes
				// @Tags Webhooks
				// @Auth open
				webhooks.GET("/logs", rc.WebhookHandler.GetLogs)
			}
		}

		// Multipart Form File Upload & Retrieval Endpoints
		if rc.UploadHandler != nil {
			// @Summary Upload file via multipart/form-data
			// @Description Ingests file(s) with size limit checks, magic-byte sniffing, and category tagging
			// @Tags Uploads
			// @Auth open
			v1.POST("/upload", rc.UploadHandler.Upload)

			// @Summary List uploaded assets catalog
			// @Description Returns metadata of all stored media files
			// @Tags Uploads
			// @Auth open
			v1.GET("/files", rc.UploadHandler.ListFiles)

			// @Summary Download or view uploaded asset
			// @Description Streams uploaded file with Content-Type header or Content-Disposition attachment
			// @Tags Uploads
			// @Auth open
			// @Param filename path string true "Stored file name"
			// @Param download query boolean false "Set to true to force file download"
			v1.GET("/files/:filename", rc.UploadHandler.GetFile)

			// @Summary Delete uploaded file
			// @Description Removes stored asset from disk and catalog
			// @Tags Uploads
			// @Auth open
			// @Param filename path string true "Stored file name"
			v1.DELETE("/files/:filename", rc.UploadHandler.DeleteFile)
		}

		// API Key Protected Routes Example
		apiKeyProtected := v1.Group("/secure")
		apiKeyProtected.Use(middleware.APIKeyAuth(rc.DB, rc.Config.MasterAPIKey))
		{
			// @Summary Enterprise secure data probe
			// @Description Requires master or database API key in X-API-Key header
			// @Tags Secure
			// @Auth api_key
			apiKeyProtected.GET("/data", func(c *gin.Context) {
				response.OK(c, gin.H{
					"message":      "Access granted via valid API Key",
					"user_id":      c.GetString("user_id"),
					"api_key_name": c.GetString("api_key_name"),
				})
			})
		}

		// Live Telementry Endpoints (System RAM / Memory Metrics & Log Stream)
		if rc.TelemetryHandler != nil {
			telemetryGroup := v1.Group("/telemetry")
			{
				// @Summary Get live telemetry snapshot and 10 memory data points
				// @Description Returns current system/VM RAM usage, Go runtime heap, and rolling window of 10 data points
				// @Tags Telemetry
				// @Auth open
				telemetryGroup.GET("/metrics", rc.TelemetryHandler.GetMetrics)

				// @Summary Simulate warning or error log
				// @Description Triggers a synthetic warning or error log entry for live stream and notification testing
				// @Tags Telemetry
				// @Auth open
				// @Body {"level": "warn", "message": "Simulated memory threshold warning", "fields": {"threshold_percent": 85}}
				telemetryGroup.POST("/simulate", rc.TelemetryHandler.SimulateLog)
			}
		}
	}

	return engine
}
