package jobs

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"hack-go-thon/pkg/log"
)

type scheduledTask struct {
	mu           sync.RWMutex
	job          Job
	interval     time.Duration
	runOnce      bool
	status       string // "scheduled", "running", "success", "failed"
	lastRun      *time.Time
	lastDuration string
	runCount     int64
	lastError    string
}

// TaskInfo represents live observability metadata for a scheduled job.
type TaskInfo struct {
	Name         string     `json:"name"`
	Interval     string     `json:"interval"`
	IntervalSec  float64    `json:"interval_seconds"`
	Status       string     `json:"status"`
	LastRun      *time.Time `json:"last_run,omitempty"`
	LastDuration string     `json:"last_duration,omitempty"`
	RunCount     int64      `json:"run_count"`
	LastError    string     `json:"last_error,omitempty"`
}

// Scheduler coordinates and executes periodic background jobs.
type Scheduler struct {
	tasks []*scheduledTask
	mu    sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	errChan chan error
}

// NewScheduler creates a new background job scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{
		tasks:   make([]*scheduledTask, 0),
		errChan: make(chan error, 50),
	}
}

// RegisterInterval registers a job to run periodically at the specified interval.
func (s *Scheduler) RegisterInterval(job Job, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task := &scheduledTask{
		job:      job,
		interval: interval,
		runOnce:  false,
		status:   "scheduled",
	}
	s.tasks = append(s.tasks, task)
	log.Info("Registered interval job", "job", job.Name(), "interval", interval.String())
}

// RegisterFunc is a convenience method to register an inline function as a periodic job.
func (s *Scheduler) RegisterFunc(name string, interval time.Duration, fn JobFunc) {
	s.RegisterInterval(NewJob(name, fn), interval)
}

// GetTasks returns a snapshot of all registered jobs and their execution metrics.
func (s *Scheduler) GetTasks() []TaskInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info := make([]TaskInfo, len(s.tasks))
	for i, t := range s.tasks {
		t.mu.RLock()
		info[i] = TaskInfo{
			Name:         t.job.Name(),
			Interval:     t.interval.String(),
			IntervalSec:  t.interval.Seconds(),
			Status:       t.status,
			LastRun:      t.lastRun,
			LastDuration: t.lastDuration,
			RunCount:     t.runCount,
			LastError:    t.lastError,
		}
		t.mu.RUnlock()
	}
	return info
}

// Errors returns a read-only channel to listen for job execution errors.
func (s *Scheduler) Errors() <-chan error {
	return s.errChan
}

// Start launches the scheduler loop in background goroutines.
func (s *Scheduler) Start(parentCtx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ctx, s.cancel = context.WithCancel(parentCtx)

	log.Info("Starting job scheduler", "total_jobs", len(s.tasks))

	for _, task := range s.tasks {
		s.wg.Add(1)
		go s.runTaskLoop(s.ctx, task)
	}

	// Drain error channel in background with logging
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.ctx.Done():
				return
			case err, ok := <-s.errChan:
				if !ok {
					return
				}
				log.Warn("Job scheduler captured error", "error", err.Error())
			}
		}
	}()
}

// Stop terminates all scheduled task loops and waits for in-flight jobs to complete.
func (s *Scheduler) Stop() {
	log.Info("Stopping job scheduler...")
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	close(s.errChan)
	log.Info("Job scheduler stopped.")
}

func (s *Scheduler) runTaskLoop(ctx context.Context, task *scheduledTask) {
	defer s.wg.Done()

	ticker := time.NewTicker(task.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Scheduled task loop exiting", "job", task.job.Name())
			return
		case <-ticker.C:
			s.executeJobSafely(ctx, task)
		}
	}
}

func (s *Scheduler) executeJobSafely(ctx context.Context, task *scheduledTask) {
	start := time.Now()
	jobName := task.job.Name()
	log.Info("Executing scheduled job", "job", jobName)

	task.mu.Lock()
	task.status = "running"
	task.mu.Unlock()

	var execErr error

	func() {
		defer func() {
			if r := recover(); r != nil {
				execErr = fmt.Errorf("job panicked: %v\nstack:\n%s", r, string(debug.Stack()))
			}
		}()

		execErr = task.job.Run(ctx)
	}()

	duration := time.Since(start)
	finishTime := time.Now().UTC()

	task.mu.Lock()
	task.runCount++
	task.lastRun = &finishTime
	task.lastDuration = duration.String()

	if execErr != nil {
		task.status = "failed"
		task.lastError = execErr.Error()
	} else {
		task.status = "success"
		task.lastError = ""
	}
	task.mu.Unlock()

	if execErr != nil {
		log.Error("Scheduled job failed",
			"job", jobName,
			"duration", duration.String(),
			"error", execErr.Error(),
		)

		select {
		case s.errChan <- fmt.Errorf("[%s] %w", jobName, execErr):
		default:
			log.Warn("Job error channel full, dropping error report", "job", jobName)
		}
	} else {
		log.Info("Scheduled job completed successfully",
			"job", jobName,
			"duration", duration.String(),
		)
	}
}
