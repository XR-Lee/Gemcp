package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultDatabaseURL = "postgres://gemcp:gemcp@localhost:5432/gemcp?sslmode=disable"

type Config struct {
	Environment              string
	Address                  string
	DatabaseURL              string
	DatabaseConnectTimeout   time.Duration
	ShutdownTimeout          time.Duration
	LogLevel                 string
	MasterKey                string
	BootstrapToken           string
	AutoMigrate              bool
	SecureCookies            bool
	PublicURL                string
	SchedulerEnabled         bool
	GlobalConcurrency        int
	SchedulerPollInterval    time.Duration
	ExecutionLeaseDuration   time.Duration
	ProvisionTimeout         time.Duration
	ReconcileDelay           time.Duration
	CallbackGrace            time.Duration
	MaxAttempts              int
	RunnerSourceMaxBytes     int64
	WatchdogPollInterval     time.Duration
	WatchdogLeaseDuration    time.Duration
	WatchdogBatchSize        int
	NotificationPollInterval time.Duration
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
	schedulerEnabled, err := boolOrDefault("GEMCP_SCHEDULER_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	globalConcurrency, err := intOrDefault("GEMCP_GLOBAL_CONCURRENCY", 1)
	if err != nil {
		return Config{}, err
	}
	maxAttempts, err := intOrDefault("GEMCP_MAX_ATTEMPTS", 3)
	if err != nil {
		return Config{}, err
	}
	sourceMaxBytes, err := int64OrDefault("GEMCP_RUNNER_SOURCE_MAX_BYTES", 256<<20)
	if err != nil {
		return Config{}, err
	}
	watchdogBatchSize, err := intOrDefault("GEMCP_WATCHDOG_BATCH_SIZE", 100)
	if err != nil {
		return Config{}, err
	}
	databaseConnectTimeout, err := durationOrDefault("GEMCP_DATABASE_CONNECT_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := durationOrDefault("GEMCP_SHUTDOWN_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	schedulerPollInterval, err := durationOrDefault("GEMCP_SCHEDULER_POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	executionLeaseDuration, err := durationOrDefault("GEMCP_EXECUTION_LEASE_DURATION", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	provisionTimeout, err := durationOrDefault("GEMCP_PROVISION_TIMEOUT", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	reconcileDelay, err := durationOrDefault("GEMCP_RECONCILE_DELAY", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	callbackGrace, err := durationOrDefault("GEMCP_CALLBACK_GRACE", 45*time.Second)
	if err != nil {
		return Config{}, err
	}
	watchdogPollInterval, err := durationOrDefault("GEMCP_WATCHDOG_POLL_INTERVAL", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	watchdogLeaseDuration, err := durationOrDefault("GEMCP_WATCHDOG_LEASE_DURATION", 2*time.Minute)
	if err != nil {
		return Config{}, err
	}
	notificationPollInterval, err := durationOrDefault("GEMCP_NOTIFICATION_POLL_INTERVAL", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Environment:              environment,
		Address:                  envOrDefault("GEMCP_ADDRESS", ":8080"),
		DatabaseURL:              envOrDefault("GEMCP_DATABASE_URL", defaultDatabaseURL),
		DatabaseConnectTimeout:   databaseConnectTimeout,
		ShutdownTimeout:          shutdownTimeout,
		LogLevel:                 strings.ToLower(envOrDefault("GEMCP_LOG_LEVEL", "info")),
		MasterKey:                strings.TrimSpace(os.Getenv("GEMCP_MASTER_KEY")),
		BootstrapToken:           strings.TrimSpace(os.Getenv("GEMCP_BOOTSTRAP_TOKEN")),
		AutoMigrate:              autoMigrate,
		SecureCookies:            secureCookies,
		PublicURL:                strings.TrimRight(strings.TrimSpace(os.Getenv("GEMCP_PUBLIC_URL")), "/"),
		SchedulerEnabled:         schedulerEnabled,
		GlobalConcurrency:        globalConcurrency,
		SchedulerPollInterval:    schedulerPollInterval,
		ExecutionLeaseDuration:   executionLeaseDuration,
		ProvisionTimeout:         provisionTimeout,
		ReconcileDelay:           reconcileDelay,
		CallbackGrace:            callbackGrace,
		MaxAttempts:              maxAttempts,
		RunnerSourceMaxBytes:     sourceMaxBytes,
		WatchdogPollInterval:     watchdogPollInterval,
		WatchdogLeaseDuration:    watchdogLeaseDuration,
		WatchdogBatchSize:        watchdogBatchSize,
		NotificationPollInterval: notificationPollInterval,
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
	if cfg.GlobalConcurrency < 1 || cfg.GlobalConcurrency > 100 {
		return Config{}, fmt.Errorf("GEMCP_GLOBAL_CONCURRENCY must be between 1 and 100")
	}
	if cfg.MaxAttempts < 1 || cfg.MaxAttempts > 10 {
		return Config{}, fmt.Errorf("GEMCP_MAX_ATTEMPTS must be between 1 and 10")
	}
	if cfg.RunnerSourceMaxBytes < 1<<20 || cfg.RunnerSourceMaxBytes > 1<<30 {
		return Config{}, fmt.Errorf("GEMCP_RUNNER_SOURCE_MAX_BYTES must be between 1 MiB and 1 GiB")
	}
	if cfg.SchedulerPollInterval <= 0 || cfg.ExecutionLeaseDuration < 30*time.Second || cfg.ProvisionTimeout <= 0 || cfg.ProvisionTimeout > 24*time.Hour || cfg.ReconcileDelay <= 0 || cfg.CallbackGrace <= 0 {
		return Config{}, fmt.Errorf("scheduler timing configuration is invalid")
	}
	if cfg.WatchdogPollInterval <= 0 || cfg.WatchdogLeaseDuration < 30*time.Second || cfg.WatchdogBatchSize < 1 || cfg.WatchdogBatchSize > 1000 {
		return Config{}, fmt.Errorf("watchdog timing configuration is invalid")
	}
	if cfg.NotificationPollInterval <= 0 {
		return Config{}, fmt.Errorf("GEMCP_NOTIFICATION_POLL_INTERVAL must be positive")
	}
	if cfg.SchedulerEnabled {
		parsed, err := url.Parse(cfg.PublicURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Trim(parsed.Path, "/") != "" {
			return Config{}, fmt.Errorf("GEMCP_PUBLIC_URL must be a credential-free HTTPS origin when scheduling is enabled")
		}
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

func intOrDefault(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return value, nil
}

func int64OrDefault(key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return value, nil
}

func durationOrDefault(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if seconds > math.MaxInt64/int64(time.Second) || seconds < math.MinInt64/int64(time.Second) {
			return 0, fmt.Errorf("%s is outside the supported duration range", key)
		}
		return time.Duration(seconds) * time.Second, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration or integer seconds", key)
	}
	return parsed, nil
}
