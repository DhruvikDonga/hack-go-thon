package main

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"hack-go-thon/config"
	"hack-go-thon/internal/api"
	"hack-go-thon/internal/api/handler"
	dbclient "hack-go-thon/internal/db_client"
	"hack-go-thon/internal/jobs"
	llmclient "hack-go-thon/internal/llm_client"
	pgstore "hack-go-thon/internal/store/pg_store"
	webrtcserver "hack-go-thon/internal/webrtc_server"
	"hack-go-thon/internal/worker"
	"hack-go-thon/internal/ws"
	"hack-go-thon/pkg/log"
)

func main() {
	// 1. Load Application Configuration
	cfg := config.Load()

	// 2. Initialize Structured Logger
	log.Init(cfg.LogLevel, cfg.Environment)
	defer log.Sync()

	log.Info("Starting application",
		"app", cfg.AppName,
		"env", cfg.Environment,
		"port", cfg.Port,
	)
	log.Info("Services configuration active",
		"database", cfg.Services.Database,
		"api_handler", cfg.Services.APIHandler,
		"rag_handler", cfg.Services.RAGHandler,
		"job_scheduler", cfg.Services.JobScheduler,
		"websocket", cfg.Services.WebSocket,
		"webrtc", cfg.Services.WebRTC,
	)

	// 3. Setup Graceful Shutdown Context
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 4. Initialize Database & LLM Clients
	var pgDB *dbclient.PostgresDatabase
	if cfg.Services.Database && cfg.PostgresURI != "" {
		var err error
		pgDB, err = dbclient.NewPostgresClient(ctx, cfg.PostgresURI, "postgres")
		if err != nil {
			log.Warn("Failed to connect to PostgreSQL", "error", err.Error())
		} else {
			if err := pgstore.InitSchema(ctx, pgDB); err != nil {
				log.Warn("Failed to initialize items schema", "error", err.Error())
			}
			if err := pgstore.InitAPIKeySchema(ctx, pgDB); err != nil {
				log.Warn("Failed to initialize api_keys schema", "error", err.Error())
			}
			if err := pgstore.InitAPICallSchema(ctx, pgDB); err != nil {
				log.Warn("Failed to initialize api_calls schema", "error", err.Error())
			}
			if err := pgstore.InitDocumentSchema(ctx, pgDB); err != nil {
				log.Warn("Failed to initialize documents schema (pgvector)", "error", err.Error())
			}
			if err := pgstore.InitUserSchema(ctx, pgDB); err != nil {
				log.Warn("Failed to initialize users schema", "error", err.Error())
			}
		}
	} else if !cfg.Services.Database {
		log.Info("Database service disabled via services.json")
	}

	var llmClient *llmclient.LLMClient
	if cfg.OpenAIKey != "" {
		llmClient = llmclient.NewClient(cfg.OpenAIKey)
	}

	// 5. Initialize Job Scheduler (if enabled)
	var scheduler *jobs.Scheduler
	if cfg.Services.JobScheduler {
		scheduler = jobs.NewScheduler()
		scheduler.RegisterInterval(jobs.NewHeartbeatJob(cfg.AppName), 30*time.Second)

		// Convenience inline job registration
		scheduler.RegisterFunc("metrics_collector", 1*time.Minute, func(ctx context.Context) error {
			log.Debug("Sample metrics collector job executed")
			return nil
		})
	} else {
		log.Info("Job scheduler service disabled via services.json")
	}

	// 6. Initialize Handlers & Services
	healthHandler := handler.NewHealthHandler()
	if pgDB != nil {
		healthHandler.RegisterChecker(pgDB)
	}

	var exampleHandler *handler.ExampleHandler
	if cfg.Services.APIHandler {
		exampleHandler = handler.NewExampleHandler(pgDB, llmClient)
	} else {
		log.Info("API resource handler service disabled via services.json")
	}

	var ragHandler *handler.RAGHandler
	if cfg.Services.RAGHandler {
		ragHandler = handler.NewRAGHandler(pgDB, llmClient)
	} else {
		log.Info("RAG handler service disabled via services.json")
	}

	var userHandler *handler.UserHandler
	if cfg.Services.APIHandler {
		userHandler = handler.NewUserHandler(pgDB, cfg.JWTSecret, cfg.TokenTTL)
	}

	// WebRTC server peer manager, SFU engine & HTTP handler (if enabled)
	var webrtcHandler *handler.WebRTCHandler
	var sfuEngine *webrtcserver.SFUEngine
	if cfg.Services.WebRTC {
		webrtcManager := webrtcserver.NewServerPeerManager(cfg)
		sfuEngine = webrtcserver.NewSFUEngine(cfg)
		webrtcHandler = handler.NewWebRTCHandler(cfg, webrtcManager, sfuEngine)
	} else {
		log.Info("WebRTC subsystem service disabled via services.json")
	}

	// Initialize WebSocket Manager using simplysocket (if enabled)
	var wsManager *ws.Manager
	if cfg.Services.WebSocket {
		adminHandler := ws.NewAdminRoomHandler(llmClient, scheduler)
		eventsHandler := ws.NewEventsRoomHandler("global", adminHandler)
		eventsHandler.SetJWTSecret(cfg.JWTSecret)
		wsManager = ws.NewManager("mesh-server", eventsHandler, adminHandler)
		wsManager.SetJWTSecret(cfg.JWTSecret)

		// Broadcast SFU track publishing events over WebSocket mesh if both are active
		if sfuEngine != nil {
			sfuEngine.SetOnTrackHook(func(roomID, publisherID, trackKind string) {
				wsManager.Broadcast("", "sfu-track-published", map[string]any{
					"room_id":      roomID,
					"publisher_id": publisherID,
					"track_kind":   trackKind,
				})
			})
		}
	} else {
		log.Info("WebSocket mesh service disabled via services.json")
	}

	router := api.SetupRouter(api.RouterConfig{
		Config:         cfg,
		HealthHandler:  healthHandler,
		ExampleHandler: exampleHandler,
		RAGHandler:     ragHandler,
		WebRTCHandler:  webrtcHandler,
		UserHandler:    userHandler,
		DB:             pgDB,
		WSManager:      wsManager,
		Scheduler:      scheduler,
	})

	// 7. Initialize & Start HTTP Server
	srv := api.NewServer(cfg, router)
	srvErrCh := srv.Start()

	// 8. Start Job Scheduler & Background Worker
	if scheduler != nil {
		scheduler.Start(ctx)
	}

	bgWorker := worker.NewBackgroundWorker("event-processor", 45*time.Second)
	go bgWorker.Run(ctx)

	// 9. Wait for Termination Signal or Server Failure
	select {
	case <-ctx.Done():
		log.Info("Shutdown signal received, initiating graceful teardown...")
	case err := <-srvErrCh:
		if err != nil {
			log.Fatal("HTTP server error encountered", "error", err.Error())
		}
	}

	// 10. Orderly Graceful Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("Server forced to shutdown", "error", err.Error())
	}

	if scheduler != nil {
		scheduler.Stop()
	}
	if pgDB != nil {
		if err := pgDB.Close(); err != nil {
			log.Warn("Error closing database connections", "error", err.Error())
		}
	}
	log.Info("Application successfully stopped")
}
