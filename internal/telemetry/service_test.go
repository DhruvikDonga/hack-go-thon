package telemetry

import (
	"context"
	"testing"
	"time"

	"hack-go-thon/pkg/log"
)

func TestCollectMemoryStats(t *testing.T) {
	pt := CollectMemoryStats()
	if pt.Timestamp == "" {
		t.Errorf("expected non-empty timestamp")
	}
	if pt.TimeUnix <= 0 {
		t.Errorf("expected valid unix timestamp, got %d", pt.TimeUnix)
	}
	if pt.SystemTotalMB <= 0 {
		t.Errorf("expected system total MB > 0, got %f", pt.SystemTotalMB)
	}
	if pt.AppSysMB <= 0 {
		t.Errorf("expected AppSysMB > 0, got %f", pt.AppSysMB)
	}
}

func TestTelemetryService_LogWorkerAndPoints(t *testing.T) {
	log.Init("debug", "development")

	svc := NewService(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc.Start(ctx)

	// Verify initial point added
	pts := svc.GetPoints()
	if len(pts) != 1 {
		t.Fatalf("expected 1 initial point, got %d", len(pts))
	}

	// Trigger sample warnings and errors
	svc.SimulateLog("warn", "test warning message", map[string]any{"source": "unit-test"})
	svc.SimulateLog("error", "test error message", map[string]any{"code": 500})

	// Wait briefly for worker to process
	time.Sleep(50 * time.Millisecond)

	totals := svc.GetTotals()
	if totals.Warnings < 1 {
		t.Errorf("expected at least 1 warning, got %d", totals.Warnings)
	}
	if totals.Errors < 1 {
		t.Errorf("expected at least 1 error, got %d", totals.Errors)
	}

	snap := svc.GetSnapshot()
	if len(snap.RecentLogs) < 2 {
		t.Errorf("expected recent logs in snapshot, got %d", len(snap.RecentLogs))
	}
}
