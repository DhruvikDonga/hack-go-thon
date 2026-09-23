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
	Config         *config.Config
	HealthHandler  *handler.HealthHandler
	ExampleHandler *handler.ExampleHandler
	RAGHandler     *handler.RAGHandler
	WebRTCHandler  *handler.WebRTCHandler
	UserHandler    *handler.UserHandler
	DB             *dbclient.PostgresDatabase
	WSManager      *ws.Manager
	Scheduler      *jobs.Scheduler
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
		// Health Probes
		health := v1.Group("/health")
		{
			health.GET("/live", rc.HealthHandler.Live)
			health.GET("/ready", rc.HealthHandler.Ready)
		}

		// WebSocket Endpoint (Single connection point for multiple RoomData logic implementations)
		if rc.WSManager != nil {
			v1.GET("/ws", rc.WSManager.Handler())

			// Live MeshServer.GetClientsInRoom() telemetry endpoint
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
				items.POST("", rc.ExampleHandler.CreateItem)
				items.GET("", rc.ExampleHandler.ListItems)
				items.GET("/:id", rc.ExampleHandler.GetItem)
			}

			// LLM Endpoints
			llm := v1.Group("/llm")
			{
				llm.POST("/ask", rc.ExampleHandler.AskAI)
			}
		}

		// Postgres pgvector RAG Endpoints
		if rc.RAGHandler != nil {
			rag := v1.Group("/rag")
			{
				rag.POST("/documents", rc.RAGHandler.CreateDocument)
				rag.GET("/documents", rc.RAGHandler.ListDocuments)
				rag.POST("/search", rc.RAGHandler.SearchDocuments)
				rag.POST("/ask", rc.RAGHandler.AskRAG)
			}
		}

		// Scheduled Background Jobs Endpoint
		if rc.Scheduler != nil {
			v1.GET("/jobs", func(c *gin.Context) {
				response.OK(c, rc.Scheduler.GetTasks())
			})
		}

		// WebRTC Signaling & Configuration Endpoints
		if rc.WebRTCHandler != nil {
			webrtcGroup := v1.Group("/webrtc")
			{
				webrtcGroup.GET("/ice-servers", rc.WebRTCHandler.GetICEServers)
				webrtcGroup.GET("/status", rc.WebRTCHandler.GetStatus)
				webrtcGroup.POST("/server/session", rc.WebRTCHandler.CreateServerSession)
				webrtcGroup.DELETE("/server/session/:id", rc.WebRTCHandler.CloseServerSession)

				// SFU Multi-Party Conference Endpoints
				sfu := webrtcGroup.Group("/sfu")
				{
					sfu.GET("/ws", rc.WebRTCHandler.HandleSFUWS)
					sfu.POST("/join", rc.WebRTCHandler.JoinSFU)
					sfu.POST("/renegotiate", rc.WebRTCHandler.RenegotiateSFU)
					sfu.POST("/leave", rc.WebRTCHandler.LeaveSFU)
					sfu.DELETE("/rooms/:room_id/peers/:peer_id", rc.WebRTCHandler.LeaveSFU)
					sfu.GET("/rooms", rc.WebRTCHandler.GetSFURooms)
				}
			}
		}

		// User & Authentication Endpoints (Multi-Level Auth & Metadata)
		if rc.UserHandler != nil {
			auth := v1.Group("/auth")
			{
				auth.POST("/register", rc.UserHandler.Register)
				auth.POST("/login", rc.UserHandler.Login)
				auth.GET("/me", middleware.JWTAuth(rc.Config.JWTSecret), rc.UserHandler.Me)
			}

			users := v1.Group("/users")
			{
				users.GET("", rc.UserHandler.ListUsers)
				users.POST("", rc.UserHandler.CreateUser)
				users.GET("/:id", rc.UserHandler.GetUser)
				users.PUT("/:id", rc.UserHandler.UpdateUser)
				users.DELETE("/:id", rc.UserHandler.DeleteUser)
				users.POST("/:id/token", rc.UserHandler.GenerateUserToken)
			}
		}

		// JWT Protected Routes Example (Multi-level verification)
		jwtProtected := v1.Group("/protected")
		jwtProtected.Use(middleware.JWTAuth(rc.Config.JWTSecret))
		{
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

		// API Key Protected Routes Example
		apiKeyProtected := v1.Group("/secure")
		apiKeyProtected.Use(middleware.APIKeyAuth(rc.DB, rc.Config.MasterAPIKey))
		{
			apiKeyProtected.GET("/data", func(c *gin.Context) {
				response.OK(c, gin.H{
					"message":      "Access granted via valid API Key",
					"user_id":      c.GetString("user_id"),
					"api_key_name": c.GetString("api_key_name"),
				})
			})
		}
	}

	return engine
}
