package telemetry

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"hack-go-thon/internal/ws"
	"hack-go-thon/pkg/log"
)

const (
	// RoomLiveTelemetry is the canonical room name for live telemetry streaming.
	RoomLiveTelemetry = "live-telemetry"
	// RoomLiveTelemetryAlias is the alternate room name matching the user specification.
	RoomLiveTelemetryAlias = "live telementry"
	// MaxDataPoints is the sliding window size for VM/System RAM metrics.
	MaxDataPoints = 10
	// MaxRecentLogs is the maximum number of recent intercepted logs cached in memory.
	MaxRecentLogs = 50
	// MetricsInterval is the cadence for collecting system RAM and memory usage.
	MetricsInterval = 30 * time.Second
)

// LogTotals summarizes intercepted high-severity log counts.
type LogTotals struct {
	Warnings uint64 `json:"warnings"`
	Errors   uint64 `json:"errors"`
	Fatal    uint64 `json:"fatal"`
	Total    uint64 `json:"total"`
}

// TelemetrySnapshot encapsulates current state for initial HTTP fetch or client room join.
type TelemetrySnapshot struct {
	Points      []MemoryDataPoint `json:"points"`
	Current     MemoryDataPoint   `json:"current"`
	Totals      LogTotals         `json:"totals"`
	RecentLogs  []log.LogEntry    `json:"recent_logs"`
	CollectedAt string            `json:"collected_at"`
}

// Service collects VM/System RAM metrics every 30s and streams non-blocking Warn+ logs.
type Service struct {
	wsManager     *ws.Manager
	logChan       chan log.LogEntry
	points        []MemoryDataPoint
	pointsMu      sync.RWMutex
	recentLogs    []log.LogEntry
	recentLogsMu  sync.RWMutex
	totalWarnings atomic.Uint64
	totalErrors   atomic.Uint64
	totalFatal    atomic.Uint64
}

// NewService creates a new Telemetry Service instance.
func NewService(wsManager *ws.Manager) *Service {
	return &Service{
		wsManager:  wsManager,
		logChan:    make(chan log.LogEntry, 1000),
		points:     make([]MemoryDataPoint, 0, MaxDataPoints),
		recentLogs: make([]log.LogEntry, 0, MaxRecentLogs),
	}
}

// Start begins the background log processor worker and 30-second memory metrics ticker.
func (s *Service) Start(ctx context.Context) {
	// Register the non-blocking sink with pkg/log
	log.RegisterLogSink(s.logChan)

	// Collect first data point immediately so initial graphs render right away
	initialPoint := CollectMemoryStats()
	s.addPoint(initialPoint)

	log.Info("Started Telementry service",
		"metrics_interval", MetricsInterval.String(),
		"max_points", MaxDataPoints,
		"room", RoomLiveTelemetry,
	)

	// Worker 1: Non-blocking log processor
	go s.runLogWorker(ctx)

	// Worker 2: 30-second system RAM and memory collector
	go s.runMetricsWorker(ctx)
}

func (s *Service) runLogWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-s.logChan:
			if !ok {
				return
			}
			s.handleLogEntry(entry)
		}
	}
}

func (s *Service) handleLogEntry(entry log.LogEntry) {
	switch entry.Level {
	case "warn", "warning":
		s.totalWarnings.Add(1)
	case "error":
		s.totalErrors.Add(1)
	case "fatal", "panic", "dpanic":
		s.totalFatal.Add(1)
	}

	s.recentLogsMu.Lock()
	if len(s.recentLogs) >= MaxRecentLogs {
		s.recentLogs = s.recentLogs[1:]
	}
	s.recentLogs = append(s.recentLogs, entry)
	s.recentLogsMu.Unlock()

	if s.wsManager != nil {
		totals := s.GetTotals()
		payload := map[string]any{
			"log":    entry,
			"totals": totals,
			"time":   time.Now().UTC().Format(time.RFC3339),
		}

		s.broadcastToTelemetryRooms("telemetry-log", payload)
	}
}

func (s *Service) runMetricsWorker(ctx context.Context) {
	ticker := time.NewTicker(MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			point := CollectMemoryStats()
			s.addPoint(point)

			if s.wsManager != nil {
				payload := map[string]any{
					"current":      point,
					"points":       s.GetPoints(),
					"totals":       s.GetTotals(),
					"collected_at": time.Now().UTC().Format(time.RFC3339),
				}

				s.broadcastToTelemetryRooms("telemetry-metrics", payload)
			}
		}
	}
}

func (s *Service) addPoint(point MemoryDataPoint) {
	s.pointsMu.Lock()
	defer s.pointsMu.Unlock()

	if len(s.points) >= MaxDataPoints {
		s.points = s.points[1:]
	}
	s.points = append(s.points, point)
}

// GetPoints returns a snapshot of up to 10 stored memory data points.
func (s *Service) GetPoints() []MemoryDataPoint {
	s.pointsMu.RLock()
	defer s.pointsMu.RUnlock()

	out := make([]MemoryDataPoint, len(s.points))
	copy(out, s.points)
	return out
}

// GetTotals returns the counts of warnings, errors, and fatal entries.
func (s *Service) GetTotals() LogTotals {
	w := s.totalWarnings.Load()
	e := s.totalErrors.Load()
	f := s.totalFatal.Load()
	return LogTotals{
		Warnings: w,
		Errors:   e,
		Fatal:    f,
		Total:    w + e + f,
	}
}

// GetRecentLogs returns a copy of the recent intercepted logs.
func (s *Service) GetRecentLogs() []log.LogEntry {
	s.recentLogsMu.RLock()
	defer s.recentLogsMu.RUnlock()

	out := make([]log.LogEntry, len(s.recentLogs))
	copy(out, s.recentLogs)
	return out
}

// GetSnapshot builds a complete telemetry state snapshot.
func (s *Service) GetSnapshot() TelemetrySnapshot {
	points := s.GetPoints()
	var current MemoryDataPoint
	if len(points) > 0 {
		current = points[len(points)-1]
	} else {
		current = CollectMemoryStats()
	}

	return TelemetrySnapshot{
		Points:      points,
		Current:     current,
		Totals:      s.GetTotals(),
		RecentLogs:  s.GetRecentLogs(),
		CollectedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// SimulateLog triggers a synthetic warning or error log for dashboard testing.
func (s *Service) SimulateLog(level, message string, fields map[string]any) {
	var kvs []any
	for k, v := range fields {
		kvs = append(kvs, k, v)
	}

	switch level {
	case "warn", "warning":
		log.Warn(message, kvs...)
	case "error":
		log.Error(message, kvs...)
	case "fatal":
		// NOTE: log.Fatal usually calls os.Exit(1).
		// For simulation, we bypass the logger to prevent crashing the server and directly inject a LogEntry.
		entry := log.LogEntry{
			Level:     "fatal",
			Timestamp: time.Now().UTC(),
			Message:   message,
			Caller:    "simulated/admin_trigger.go:1",
			Fields:    fields,
		}
		s.handleLogEntry(entry)
	default:
		log.Warn(message, kvs...)
	}
}

func (s *Service) broadcastToTelemetryRooms(action string, payload map[string]any) {
	if s.wsManager == nil {
		return
	}
	s.wsManager.Broadcast(RoomLiveTelemetry, action, payload)
}
