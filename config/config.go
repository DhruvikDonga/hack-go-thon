package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the application.
type Config struct {
	AppName         string
	Port            string
	LogLevel        string
	Environment     string
	ShutdownTimeout time.Duration
	AllowedOrigins  []string
	PostgresURI     string
	OpenAIKey       string
	JWTSecret       string
	TokenTTL        time.Duration
	MasterAPIKey    string
	STUNServers     []string
	TURNServerURL   string
	TURNUsername    string
	TURNCredential  string
	Services        ServicesConfig
}

// Load reads application configuration from environment variables with fallback defaults.
func Load() *Config {
	appName := getEnv("APP_NAME", "hack-go-thon")
	port := getEnv("PORT", ":8080")
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	logLevel := getEnv("LOG_LEVEL", "debug")
	environment := getEnv("ENV", "development")

	timeoutSec := getEnvAsInt("SHUTDOWN_TIMEOUT_SECONDS", 10)
	shutdownTimeout := time.Duration(timeoutSec) * time.Second

	originsStr := getEnv("CORS_ALLOWED_ORIGINS", "*")
	var allowedOrigins []string
	if originsStr == "*" {
		allowedOrigins = []string{"*"}
	} else {
		for _, o := range strings.Split(originsStr, ",") {
			trimmed := strings.TrimSpace(o)
			if trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}

	pgURI := getEnv("PG_URI", "")
	if pgURI == "" {
		pgURI = getEnv("DATABASE_URL", "")
	}

	openAIKey := getEnv("OPENAI_API_KEY", "")
	if openAIKey == "" {
		openAIKey = getEnv("OPEN_AI_KEY", "")
	}

	jwtSecret := getEnv("JWT_SECRET", "default-dev-jwt-secret")
	jwtTTLHours := getEnvAsInt("JWT_TTL_HOURS", 24)
	tokenTTL := time.Duration(jwtTTLHours) * time.Hour
	masterAPIKey := getEnv("MASTER_API_KEY", "")

	stunStr := getEnv("STUN_SERVERS", "stun:stun.l.google.com:19302,stun:stun1.l.google.com:19302")
	var stunServers []string
	for _, s := range strings.Split(stunStr, ",") {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			stunServers = append(stunServers, trimmed)
		}
	}
	turnURL := getEnv("TURN_SERVER_URL", "")
	turnUser := getEnv("TURN_USERNAME", "")
	turnCred := getEnv("TURN_CREDENTIAL", "")
	servicesCfg := LoadServicesConfig("")

	return &Config{
		AppName:         appName,
		Port:            port,
		LogLevel:        logLevel,
		Environment:     environment,
		ShutdownTimeout: shutdownTimeout,
		AllowedOrigins:  allowedOrigins,
		PostgresURI:     pgURI,
		OpenAIKey:       openAIKey,
		JWTSecret:       jwtSecret,
		TokenTTL:        tokenTTL,
		MasterAPIKey:    masterAPIKey,
		STUNServers:     stunServers,
		TURNServerURL:   turnURL,
		TURNUsername:    turnUser,
		TURNCredential:  turnCred,
		Services:        servicesCfg,
	}
}

func getEnv(key, defaultVal string) string {
	if val, exists := os.LookupEnv(key); exists && val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valStr := getEnv(key, "")
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}
