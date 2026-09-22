package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"hack-go-thon/pkg/log"
)

func init() {
	log.Init("error", "test")
}

func TestScheduler(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scheduler := NewScheduler()

	var counter int32
	scheduler.RegisterFunc("incrementer", 50*time.Millisecond, func(ctx context.Context) error {
		atomic.AddInt32(&counter, 1)
		return nil
	})

	// Register a job that intentionally panics to test resilience
	scheduler.RegisterFunc("panicker", 60*time.Millisecond, func(ctx context.Context) error {
		panic("simulated panic")
	})

	scheduler.Start(ctx)

	// Allow jobs to run multiple times
	time.Sleep(180 * time.Millisecond)

	scheduler.Stop()

	// Check GetTasks observability after stopping
	tasks := scheduler.GetTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks in scheduler, got %d", len(tasks))
	}

	var foundIncrementer, foundPanicker bool
	for _, task := range tasks {
		if task.Name == "incrementer" {
			foundIncrementer = true
			if task.RunCount == 0 {
				t.Errorf("expected incrementer runCount > 0")
			}
			if task.Status != "success" {
				t.Errorf("expected incrementer status success, got %s", task.Status)
			}
		}
		if task.Name == "panicker" {
			foundPanicker = true
			if task.Status != "failed" {
				t.Errorf("expected panicker status failed, got %s", task.Status)
			}
			if task.LastError == "" {
				t.Errorf("expected panicker lastError to be non-empty")
			}
		}
	}

	if !foundIncrementer || !foundPanicker {
		t.Errorf("missing expected tasks in GetTasks output")
	}

	count := atomic.LoadInt32(&counter)
	if count < 2 {
		t.Errorf("expected counter >= 2, got %d", count)
	}
}
