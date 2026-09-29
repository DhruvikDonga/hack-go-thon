package log

import (
	"testing"
	"time"
)

func TestTelemetrySink_FiltersDebugAndInfo(t *testing.T) {
	Init("debug", "development")

	sink := make(chan LogEntry, 10)
	RegisterLogSink(sink)

	// These should be ignored by the telemetry sink
	Debug("this is a debug log", "debug_key", "val1")
	Info("this is an info log", "info_key", "val2")

	// These should be captured
	Warn("warning alert: high load", "metric", "cpu", "usage", 88)
	Error("critical error: db timeout", "attempt", 3)

	var entries []LogEntry
	timeout := time.After(200 * time.Millisecond)

collect:
	for {
		select {
		case e := <-sink:
			entries = append(entries, e)
			if len(entries) == 2 {
				break collect
			}
		case <-timeout:
			break collect
		}
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries (warn & error), got %d", len(entries))
	}

	if entries[0].Level != "warn" || entries[0].Message != "warning alert: high load" {
		t.Errorf("unexpected first entry: %+v", entries[0])
	}
	if entries[0].Fields["metric"] != "cpu" {
		t.Errorf("expected field metric=cpu, got %v", entries[0].Fields["metric"])
	}

	if entries[1].Level != "error" || entries[1].Message != "critical error: db timeout" {
		t.Errorf("unexpected second entry: %+v", entries[1])
	}
}

func TestTelemetrySink_NonBlockingWhenFull(t *testing.T) {
	Init("debug", "development")

	// Buffer capacity 1
	sink := make(chan LogEntry, 1)
	RegisterLogSink(sink)

	// Send multiple warnings. If blocking, this would hang.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			Warn("warning flood", "iter", i)
		}
		close(done)
	}()

	select {
	case <-done:
		// Succeeded without deadlock or blocking
	case <-time.After(500 * time.Millisecond):
		t.Fatal("logging blocked when sink channel was full")
	}
}
