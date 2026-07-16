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
	MasterKey              string
	BootstrapToken         string
	AutoMigrate            bool
	SecureCookies          bool
}

func Load() (Config, error) {
	environment := envOrDefault("GEMCP_ENV", "development")
	autoMigrate, err := boolOrDefault("GEMCP_AUTO_MIGRATE", false)
	if err != nil {
		return Config{}, err
	}
	secureCookies, err := boolOrDefault("GEMCP_SECURE_COOKIES", environment == "production")
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Environment:            environment,
		Address:                envOrDefault("GEMCP_ADDRESS", ":8080"),
		DatabaseURL:            envOrDefault("GEMCP_DATABASE_URL", defaultDatabaseURL),
		DatabaseConnectTimeout: durationOrDefault("GEMCP_DATABASE_CONNECT_TIMEOUT", 10*time.Second),
		ShutdownTimeout:        durationOrDefault("GEMCP_SHUTDOWN_TIMEOUT", 15*time.Second),
		LogLevel:               strings.ToLower(envOrDefault("GEMCP_LOG_LEVEL", "info")),
		MasterKey:              strings.TrimSpace(os.Getenv("GEMCP_MASTER_KEY")),
		BootstrapToken:         strings.TrimSpace(os.Getenv("GEMCP_BOOTSTRAP_TOKEN")),
		AutoMigrate:            autoMigrate,
		SecureCookies:          secureCookies,
	}

	if !strings.HasPrefix(cfg.Address, ":") && !strings.Contains(cfg.Address, ":") {
		return Config{}, fmt.Errorf("GEMCP_ADDRESS must be host:port or :port")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("GEMCP_DATABASE_URL is required")
	}
	if cfg.BootstrapToken != "" && len(cfg.BootstrapToken) < 32 {
		return Config{}, fmt.Errorf("GEMCP_BOOTSTRAP_TOKEN must contain at least 32 characters")
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

func boolOrDefault(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return value, nil
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
