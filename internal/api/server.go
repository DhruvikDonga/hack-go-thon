package api

import (
	"context"
	"net/http"
	"time"

	"hack-go-thon/config"
	"hack-go-thon/pkg/log"
)

// Server encapsulates the HTTP server lifecycle.
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
}

// NewServer creates a new API Server instance.
func NewServer(cfg *config.Config, handler http.Handler) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:         cfg.Port,
			Handler:      handler,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		cfg: cfg,
	}
}

// Start runs the HTTP server asynchronously and returns an error channel.
func (s *Server) Start() <-chan error {
	errCh := make(chan error, 1)

	go func() {
		log.Info("HTTP server listening", "addr", s.cfg.Port, "env", s.cfg.Environment)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	return errCh
}

// Shutdown gracefully stops the HTTP server within the given context.
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info("Shutting down HTTP server gracefully...")
	return s.httpServer.Shutdown(ctx)
}
