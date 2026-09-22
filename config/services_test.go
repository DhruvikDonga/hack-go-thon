package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultServicesConfig(t *testing.T) {
	cfg := DefaultServicesConfig()
	if !cfg.Database {
		t.Errorf("expected Database to default to true")
	}
	if !cfg.APIHandler {
		t.Errorf("expected APIHandler to default to true")
	}
	if !cfg.RAGHandler {
		t.Errorf("expected RAGHandler to default to true")
	}
	if !cfg.JobScheduler {
		t.Errorf("expected JobScheduler to default to true")
	}
	if !cfg.WebSocket {
		t.Errorf("expected WebSocket to default to true")
	}
	if !cfg.WebRTC {
		t.Errorf("expected WebRTC to default to true")
	}
}

func TestLoadServicesConfig_MissingFile(t *testing.T) {
	cfg := LoadServicesConfig("/nonexistent/services.json")
	if !cfg.Database || !cfg.APIHandler || !cfg.RAGHandler || !cfg.JobScheduler || !cfg.WebSocket || !cfg.WebRTC {
		t.Errorf("expected all services to be true when config file is missing, got %+v", cfg)
	}
}

func TestLoadServicesConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(filePath, []byte("{not-valid-json"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	cfg := LoadServicesConfig(filePath)
	if !cfg.Database || !cfg.APIHandler || !cfg.RAGHandler || !cfg.JobScheduler || !cfg.WebSocket || !cfg.WebRTC {
		t.Errorf("expected all services to default to true on invalid JSON, got %+v", cfg)
	}
}

func TestLoadServicesConfig_ExplicitFlags(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "services.json")
	content := []byte(`{
		"database": false,
		"api_handler": false,
		"rag_handler": true,
		"job_scheduler": false,
		"websocket": true,
		"webrtc": false
	}`)
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	cfg := LoadServicesConfig(filePath)
	if cfg.Database {
		t.Errorf("expected Database to be false")
	}
	if cfg.APIHandler {
		t.Errorf("expected APIHandler to be false")
	}
	if !cfg.RAGHandler {
		t.Errorf("expected RAGHandler to be true")
	}
	if cfg.JobScheduler {
		t.Errorf("expected JobScheduler to be false")
	}
	if !cfg.WebSocket {
		t.Errorf("expected WebSocket to be true")
	}
	if cfg.WebRTC {
		t.Errorf("expected WebRTC to be false")
	}
}

func TestLoadServicesConfig_Aliases(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "services_alias.json")
	content := []byte(`{
		"db": false,
		"api": false,
		"rag": false,
		"scheduler": false,
		"ws": false,
		"sfu": false
	}`)
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	cfg := LoadServicesConfig(filePath)
	if cfg.Database {
		t.Errorf("expected Database to be false via 'db' alias")
	}
	if cfg.APIHandler {
		t.Errorf("expected APIHandler to be false via 'api' alias")
	}
	if cfg.RAGHandler {
		t.Errorf("expected RAGHandler to be false via 'rag' alias")
	}
	if cfg.JobScheduler {
		t.Errorf("expected JobScheduler to be false via 'scheduler' alias")
	}
	if cfg.WebSocket {
		t.Errorf("expected WebSocket to be false via 'ws' alias")
	}
	if cfg.WebRTC {
		t.Errorf("expected WebRTC to be false via 'sfu' alias")
	}
}

func TestLoadServicesConfig_EnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "env_services.json")
	content := []byte(`{
		"websocket": false
	}`)
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	t.Setenv("SERVICES_CONFIG_PATH", filePath)
	cfg := LoadServicesConfig("")

	if cfg.WebSocket {
		t.Errorf("expected WebSocket to be false from SERVICES_CONFIG_PATH")
	}
	if !cfg.Database || !cfg.WebRTC {
		t.Errorf("expected unmentioned services to remain true")
	}
}
