package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultDatabaseURL = "postgres://gemcp:gemcp@localhost:5432/gemcp?sslmode=disable"

type Config struct {
	Environment            string
	Address                string
	DatabaseURL            string
	DatabaseConnectTimeout time.Duration
	ShutdownTimeout        time.Duration
	LogLevel               string
}

func Load() (Config, error) {
	cfg := Config{
		Environment:            envOrDefault("GEMCP_ENV", "development"),
		Address:                envOrDefault("GEMCP_ADDRESS", ":8080"),
		DatabaseURL:            envOrDefault("GEMCP_DATABASE_URL", defaultDatabaseURL),
		DatabaseConnectTimeout: durationOrDefault("GEMCP_DATABASE_CONNECT_TIMEOUT", 10*time.Second),
		ShutdownTimeout:        durationOrDefault("GEMCP_SHUTDOWN_TIMEOUT", 15*time.Second),
		LogLevel:               strings.ToLower(envOrDefault("GEMCP_LOG_LEVEL", "info")),
	}

	if !strings.HasPrefix(cfg.Address, ":") && !strings.Contains(cfg.Address, ":") {
		return Config{}, fmt.Errorf("GEMCP_ADDRESS must be host:port or :port")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("GEMCP_DATABASE_URL is required")
	}
	if cfg.DatabaseConnectTimeout <= 0 {
		return Config{}, fmt.Errorf("GEMCP_DATABASE_CONNECT_TIMEOUT must be positive")
	}
	if cfg.ShutdownTimeout <= 0 {
		return Config{}, fmt.Errorf("GEMCP_SHUTDOWN_TIMEOUT must be positive")
	}
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if parsed, err := time.ParseDuration(raw); err == nil {
		return parsed
	}
	return fallback
}
