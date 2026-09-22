package jobs

import (
	"context"
	"time"

	"hack-go-thon/pkg/log"
)

// HeartbeatJob periodically logs system health and heartbeats.
type HeartbeatJob struct {
	ServiceName string
}

// NewHeartbeatJob creates an instance of HeartbeatJob.
func NewHeartbeatJob(serviceName string) *HeartbeatJob {
	return &HeartbeatJob{
		ServiceName: serviceName,
	}
}

func (j *HeartbeatJob) Name() string {
	return "heartbeat_job"
}

func (j *HeartbeatJob) Run(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	log.Info("Heartbeat check executed",
		"service", j.ServiceName,
		"timestamp", time.Now().UTC().Format(time.RFC3339),
	)

	return nil
}
