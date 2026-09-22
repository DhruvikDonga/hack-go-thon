package jobs

import (
	"context"
	"time"
)

// Job defines the contract for any executable background task.
type Job interface {
	Name() string
	Run(ctx context.Context) error
}

// JobFunc is an adapter to allow the use of ordinary functions as Jobs.
type JobFunc func(ctx context.Context) error

type funcJob struct {
	name string
	fn   JobFunc
}

func (f *funcJob) Name() string {
	return f.name
}

func (f *funcJob) Run(ctx context.Context) error {
	return f.fn(ctx)
}

// NewJob creates a Job from a name and a function.
func NewJob(name string, fn JobFunc) Job {
	return &funcJob{
		name: name,
		fn:   fn,
	}
}

// ExecutionRecord captures metadata of a job execution.
type ExecutionRecord struct {
	JobName   string        `json:"job_name"`
	StartTime time.Time     `json:"start_time"`
	Duration  time.Duration `json:"duration"`
	Success   bool          `json:"success"`
	Error     string        `json:"error,omitempty"`
}
