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

	"github.com/gin-gonic/gin"
)

// RouterConfig contains dependencies for building the HTTP router.
type RouterConfig struct {
	Config         *config.Config
	HealthHandler  *handler.HealthHandler
	ExampleHandler *handler.ExampleHandler
	RAGHandler     *handler.RAGHandler
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
		}

		// Public Example Resource Endpoints
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

		// JWT Protected Routes Example
		jwtProtected := v1.Group("/protected")
		jwtProtected.Use(middleware.JWTAuth(rc.Config.JWTSecret))
		{
			jwtProtected.GET("/profile", func(c *gin.Context) {
				response.OK(c, gin.H{
					"message": "Access granted via valid JWT",
					"user_id": c.GetString("user_id"),
					"email":   c.GetString("email"),
					"role":    c.GetString("role"),
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
