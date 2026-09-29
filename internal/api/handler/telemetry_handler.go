package handler

import (
	"strings"

	"hack-go-thon/internal/telemetry"
	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
)

// TelemetryHandler exposes HTTP endpoints for system memory telemetry and test log simulation.
type TelemetryHandler struct {
	service *telemetry.Service
}

// NewTelemetryHandler creates a new TelemetryHandler.
func NewTelemetryHandler(service *telemetry.Service) *TelemetryHandler {
	return &TelemetryHandler{
		service: service,
	}
}

// GetMetrics returns the current 10-point system RAM and Go memory metrics snapshot.
func (h *TelemetryHandler) GetMetrics(c *gin.Context) {
	if h.service == nil {
		response.Error(c, apperrors.NewInternal("Telemetry service is not configured", nil))
		return
	}

	snapshot := h.service.GetSnapshot()
	response.OK(c, snapshot)
}

// SimulateLogRequest defines the request body for triggering synthetic warning or error logs.
type SimulateLogRequest struct {
	Level   string         `json:"level"`   // "warn" or "error"
	Message string         `json:"message"` // Message text
	Fields  map[string]any `json:"fields"`  // Arbitrary contextual JSON fields
}

// SimulateLog emits a synthetic warning or error log into the system for live dashboard verification.
func (h *TelemetryHandler) SimulateLog(c *gin.Context) {
	if h.service == nil {
		response.Error(c, apperrors.NewInternal("Telemetry service is not configured", nil))
		return
	}

	var req SimulateLogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Fallback defaults if body is empty or malformed
		req = SimulateLogRequest{
			Level:   "warn",
			Message: "Simulated warning alert: high memory threshold reached",
			Fields:  map[string]any{"source": "admin-panel", "simulated": true},
		}
	}

	req.Level = strings.ToLower(strings.TrimSpace(req.Level))
	if req.Level != "warn" && req.Level != "error" && req.Level != "fatal" {
		req.Level = "warn"
	}
	if req.Message == "" {
		req.Message = "Simulated " + req.Level + " log event from admin panel"
	}
	if req.Fields == nil {
		req.Fields = map[string]any{"source": "admin-panel", "simulated": true}
	}

	h.service.SimulateLog(req.Level, req.Message, req.Fields)

	response.OK(c, gin.H{
		"emitted": true,
		"level":   req.Level,
		"message": req.Message,
		"fields":  req.Fields,
	})
}
