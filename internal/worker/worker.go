package worker

import (
	"context"
	"time"

	"hack-go-thon/pkg/log"
)

// Worker represents a background processor that runs continuously until context cancellation.
type Worker interface {
	Name() string
	Run(ctx context.Context)
}

// BackgroundWorker demonstrates a continuous worker loop with ticker and cancellation support.
type BackgroundWorker struct {
	name     string
	interval time.Duration
}

// NewBackgroundWorker creates a new BackgroundWorker instance.
func NewBackgroundWorker(name string, interval time.Duration) *BackgroundWorker {
	return &BackgroundWorker{
		name:     name,
		interval: interval,
	}
}

func (w *BackgroundWorker) Name() string {
	return w.name
}

// Run executes the worker's processing loop.
func (w *BackgroundWorker) Run(ctx context.Context) {
	log.Info("Worker started", "worker", w.name, "interval", w.interval.String())

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Initial execution on startup
	w.process(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Info("Worker received termination signal, stopping...", "worker", w.name)
			return
		case <-ticker.C:
			w.process(ctx)
		}
	}
}

func (w *BackgroundWorker) process(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	log.Debug("Worker processing cycle executed", "worker", w.name)
}
