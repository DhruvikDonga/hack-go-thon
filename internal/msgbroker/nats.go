package msgbroker

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/nats-io/nats.go"
)

var NC *nats.Conn

// InitNATS connects to the NATS server.
func InitNATS() {
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = nats.DefaultURL
	}

	nc, err := nats.Connect(url)
	if err != nil {
		log.Printf("Warning: Failed to connect to NATS at %s: %v", url, err)
		return
	}

	NC = nc
	log.Printf("Successfully connected to NATS at %s", url)
}

// Publish broadcasts an event to a NATS subject.
func Publish(subject string, data []byte) error {
	if NC == nil {
		return nil // Ignore if NATS isn't connected
	}
	return NC.Publish(subject, data)
}

// Close gracefully closes the NATS connection.
func Close() {
	if NC != nil {
		NC.Close()
	}
}

// Subscribe listens for events on a NATS subject.
func Subscribe(subject string, handler nats.MsgHandler) (*nats.Subscription, error) {
	if NC == nil {
		return nil, fmt.Errorf("NATS is not connected")
	}
	return NC.Subscribe(subject, handler)
}

// NATSChecker implements the handler.Checker interface
type NATSChecker struct{}

func (c *NATSChecker) Name() string {
	return "nats"
}

func (c *NATSChecker) Check(ctx context.Context) error {
	if NC == nil {
		return fmt.Errorf("NATS is not connected")
	}
	if NC.IsClosed() {
		return fmt.Errorf("NATS connection is closed")
	}
	return nil
}

func NewChecker() *NATSChecker {
	return &NATSChecker{}
}
