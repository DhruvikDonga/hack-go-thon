package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
)

// Checker represents a health check probe for an external dependency (e.g. DB, Cache).
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

// HealthHandler provides HTTP endpoints for liveness and readiness probes.
type HealthHandler struct {
	checkers []Checker
	mu       sync.RWMutex
}

// NewHealthHandler creates a new HealthHandler instance.
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{
		checkers: make([]Checker, 0),
	}
}

// RegisterChecker adds a dependency health checker to readiness probes.
func (h *HealthHandler) RegisterChecker(c Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers = append(h.checkers, c)
}

// Live godoc
// @Summary Liveness probe
// @Description Confirms that the HTTP server process is running.
// @Tags health
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/health/live [get]
func (h *HealthHandler) Live(c *gin.Context) {
	response.OK(c, gin.H{
		"status": "alive",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// Ready godoc
// @Summary Readiness probe
// @Description Confirms whether all required dependencies are healthy and ready to serve traffic.
// @Tags health
// @Produce json
// @Success 200 {object} response.Response
// @Failure 503 {object} response.Response
// @Router /api/v1/health/ready [get]
func (h *HealthHandler) Ready(c *gin.Context) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	services := make(map[string]string)
	isReady := true

	for _, checker := range h.checkers {
		if err := checker.Check(ctx); err != nil {
			services[checker.Name()] = "down: " + err.Error()
			isReady = false
		} else {
			services[checker.Name()] = "up"
		}
	}

	status := "ready"
	httpStatus := http.StatusOK
	if !isReady {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
	}

	c.JSON(httpStatus, response.Response{
		Success: isReady,
		Data: gin.H{
			"status":   status,
			"services": services,
			"time":     time.Now().UTC().Format(time.RFC3339),
		},
	})
}
