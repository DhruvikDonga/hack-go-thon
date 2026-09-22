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

	// 3. Setup Graceful Shutdown Context
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 4. Initialize Database & LLM Clients
	var pgDB *dbclient.PostgresDatabase
	if cfg.PostgresURI != "" {
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
		}
	}

	var llmClient *llmclient.LLMClient
	if cfg.OpenAIKey != "" {
		llmClient = llmclient.NewClient(cfg.OpenAIKey)
	}

	// 5. Initialize Job Scheduler
	scheduler := jobs.NewScheduler()
	scheduler.RegisterInterval(jobs.NewHeartbeatJob(cfg.AppName), 30*time.Second)

	// Convenience inline job registration
	scheduler.RegisterFunc("metrics_collector", 1*time.Minute, func(ctx context.Context) error {
		log.Debug("Sample metrics collector job executed")
		return nil
	})

	// 6. Initialize Handlers & WebSocket Manager
	healthHandler := handler.NewHealthHandler()
	if pgDB != nil {
		healthHandler.RegisterChecker(pgDB)
	}

	exampleHandler := handler.NewExampleHandler(pgDB, llmClient)
	ragHandler := handler.NewRAGHandler(pgDB, llmClient)

	// Initialize WebSocket Manager using simplysocket with AdminRoomHandler wired to scheduler
	adminHandler := ws.NewAdminRoomHandler(llmClient, scheduler)
	wsManager := ws.NewManager("mesh-server", ws.NewEventsRoomHandler("global", adminHandler), adminHandler)

	router := api.SetupRouter(api.RouterConfig{
		Config:         cfg,
		HealthHandler:  healthHandler,
		ExampleHandler: exampleHandler,
		RAGHandler:     ragHandler,
		DB:             pgDB,
		WSManager:      wsManager,
		Scheduler:      scheduler,
	})

	// 7. Initialize & Start HTTP Server
	srv := api.NewServer(cfg, router)
	srvErrCh := srv.Start()

	// 8. Start Job Scheduler & Background Worker
	scheduler.Start(ctx)

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

	scheduler.Stop()
	if pgDB != nil {
		if err := pgDB.Close(); err != nil {
			log.Warn("Error closing database connections", "error", err.Error())
		}
	}
	log.Info("Application successfully stopped")
}
