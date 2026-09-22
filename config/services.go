package config

import (
	"encoding/json"
	"os"
	"strings"
)

// ServicesConfig defines feature flags to selectively initialize subsystems.
// All services default to true unless explicitly configured as false in services.json.
type ServicesConfig struct {
	Database     bool `json:"database"`
	APIHandler   bool `json:"api_handler"`
	RAGHandler   bool `json:"rag_handler"`
	JobScheduler bool `json:"job_scheduler"`
	WebSocket    bool `json:"websocket"`
	WebRTC       bool `json:"webrtc"`
}

// DefaultServicesConfig returns a ServicesConfig with all subsystems enabled.
func DefaultServicesConfig() ServicesConfig {
	return ServicesConfig{
		Database:     true,
		APIHandler:   true,
		RAGHandler:   true,
		JobScheduler: true,
		WebSocket:    true,
		WebRTC:       true,
	}
}

// LoadServicesConfig reads the services configuration from the specified JSON file path.
// If path is empty, it checks the SERVICES_CONFIG_PATH environment variable, falling back to "services.json".
// Missing keys or missing files default to true.
func LoadServicesConfig(filePath string) ServicesConfig {
	cfg := DefaultServicesConfig()

	if filePath == "" {
		filePath = os.Getenv("SERVICES_CONFIG_PATH")
		if filePath == "" {
			filePath = "services.json"
		}
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return cfg
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg
	}

	// Helper to extract boolean value across multiple aliases
	checkBool := func(keys ...string) (bool, bool) {
		for _, k := range keys {
			lowerK := strings.ToLower(k)
			for rawKey, rawVal := range raw {
				if strings.ToLower(rawKey) == lowerK {
					if b, ok := rawVal.(bool); ok {
						return b, true
					}
				}
			}
		}
		return true, false
	}

	if val, ok := checkBool("database", "db", "postgres"); ok {
		cfg.Database = val
	}
	if val, ok := checkBool("api_handler", "api", "example_handler", "apihandler"); ok {
		cfg.APIHandler = val
	}
	if val, ok := checkBool("rag_handler", "rag", "raghandler"); ok {
		cfg.RAGHandler = val
	}
	if val, ok := checkBool("job_scheduler", "scheduler", "jobs", "jobscheduler"); ok {
		cfg.JobScheduler = val
	}
	if val, ok := checkBool("websocket", "websockets", "ws"); ok {
		cfg.WebSocket = val
	}
	if val, ok := checkBool("webrtc", "webrtc_server", "sfu"); ok {
		cfg.WebRTC = val
	}

	return cfg
}
