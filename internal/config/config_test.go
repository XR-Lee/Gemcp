package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("GEMCP_ENV", "")
	t.Setenv("GEMCP_ADDRESS", "")
	t.Setenv("GEMCP_DATABASE_URL", "")
	t.Setenv("GEMCP_DATABASE_CONNECT_TIMEOUT", "")
	t.Setenv("GEMCP_SHUTDOWN_TIMEOUT", "")
	t.Setenv("GEMCP_AUTO_MIGRATE", "")
	t.Setenv("GEMCP_SECURE_COOKIES", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Address != ":8080" {
		t.Fatalf("Address = %q, want :8080", cfg.Address)
	}
	if cfg.DatabaseConnectTimeout != 10*time.Second {
		t.Fatalf("DatabaseConnectTimeout = %s", cfg.DatabaseConnectTimeout)
	}
	if !cfg.AutoMigrate {
		t.Fatal("development AutoMigrate defaulted to false")
	}
	if cfg.SelfHostedEnabled {
		t.Fatal("Self-hosted nodes defaulted to enabled")
	}
	if cfg.SSHCloudEnabled {
		t.Fatal("Cloud SSH nodes defaulted to enabled")
	}
}

func TestLoadProductionDefaultsDisableAutoMigrate(t *testing.T) {
	t.Setenv("GEMCP_ENV", "production")
	t.Setenv("GEMCP_AUTO_MIGRATE", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoMigrate {
		t.Fatal("production AutoMigrate defaulted to true")
	}
}

func TestLoadRejectsExamplePlaceholders(t *testing.T) {
	t.Setenv("GEMCP_MASTER_KEY", "generate-with-gemcp-keygen")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted the example master key placeholder")
	}
	t.Setenv("GEMCP_MASTER_KEY", "")
	t.Setenv("GEMCP_BOOTSTRAP_TOKEN", "generate-with-gemcp-bootstrap-token")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted the example bootstrap token placeholder")
	}
}

func TestLoadRejectsShortBootstrapToken(t *testing.T) {
	t.Setenv("GEMCP_BOOTSTRAP_TOKEN", "too-short")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted short bootstrap token")
	}
}

func TestLoadRejectsInvalidBoolean(t *testing.T) {
	t.Setenv("GEMCP_AUTO_MIGRATE", "sometimes")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted invalid boolean")
	}
}

func TestSelfHostedNodesAreOptInAndRequireHTTPSPublicOrigin(t *testing.T) {
	t.Setenv("GEMCP_SELF_HOSTED_ENABLED", "true")
	t.Setenv("GEMCP_PUBLIC_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted Self-hosted nodes without a public HTTPS origin")
	}
	t.Setenv("GEMCP_PUBLIC_URL", "http://gemcp.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted Self-hosted nodes with a public HTTP origin")
	}
	t.Setenv("GEMCP_PUBLIC_URL", "http://127.0.0.1:18080")
	cfg, err := Load()
	if err != nil || !cfg.SelfHostedEnabled {
		t.Fatalf("loopback Self-hosted config=%+v err=%v", cfg, err)
	}
	t.Setenv("GEMCP_PUBLIC_URL", "https://gemcp.example.com")
	cfg, err = Load()
	if err != nil || !cfg.SelfHostedEnabled {
		t.Fatalf("Self-hosted config=%+v err=%v", cfg, err)
	}
}

func TestSchedulerIsOptInAndRequiresHTTPSPublicOrigin(t *testing.T) {
	t.Setenv("GEMCP_SCHEDULER_ENABLED", "")
	t.Setenv("GEMCP_PUBLIC_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchedulerEnabled {
		t.Fatal("scheduler defaulted to enabled")
	}

	t.Setenv("GEMCP_SCHEDULER_ENABLED", "true")
	t.Setenv("GEMCP_PUBLIC_URL", "http://gemcp.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an HTTP Runner origin")
	}
	t.Setenv("GEMCP_PUBLIC_URL", "http://127.0.0.1:18080")
	cfg, err = Load()
	if err != nil || !cfg.SchedulerEnabled {
		t.Fatalf("loopback scheduler config = %+v, %v", cfg, err)
	}
	if HTTPSPublicOrigin(cfg.PublicURL) {
		t.Fatal("loopback HTTP origin was treated as an HTTPS callback origin")
	}
	t.Setenv("GEMCP_PUBLIC_URL", "https://gemcp.example.com")
	cfg, err = Load()
	if err != nil || !cfg.SchedulerEnabled || !HTTPSPublicOrigin(cfg.PublicURL) {
		t.Fatalf("enabled scheduler config = %+v, %v", cfg, err)
	}
}

func TestSSHCloudNodesAreOptIn(t *testing.T) {
	t.Setenv("GEMCP_SSH_CLOUD_ENABLED", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SSHCloudEnabled {
		t.Fatal("Cloud SSH nodes defaulted to enabled")
	}
	t.Setenv("GEMCP_SSH_CLOUD_ENABLED", "true")
	cfg, err = Load()
	if err != nil || !cfg.SSHCloudEnabled {
		t.Fatalf("Cloud SSH config=%+v err=%v", cfg, err)
	}
}

func TestExecutionBoundsAreValidated(t *testing.T) {
	t.Setenv("GEMCP_GLOBAL_CONCURRENCY", "0")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted zero global concurrency")
	}
	t.Setenv("GEMCP_GLOBAL_CONCURRENCY", "1")
	t.Setenv("GEMCP_RUNNER_SOURCE_MAX_BYTES", "1024")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a tiny source archive limit")
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("GEMCP_WATCHDOG_POLL_INTERVAL", "eventually")
	if _, err := Load(); err == nil {
		t.Fatal("Load() silently accepted an invalid duration")
	}
}

func TestLoadRejectsOverflowingIntegerDuration(t *testing.T) {
	t.Setenv("GEMCP_WATCHDOG_POLL_INTERVAL", "9223372036854775807")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an overflowing integer duration")
	}
}

func TestLoadDurations(t *testing.T) {
	t.Setenv("GEMCP_DATABASE_CONNECT_TIMEOUT", "3s")
	t.Setenv("GEMCP_SHUTDOWN_TIMEOUT", "20")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DatabaseConnectTimeout != 3*time.Second {
		t.Fatalf("DatabaseConnectTimeout = %s", cfg.DatabaseConnectTimeout)
	}
	if cfg.ShutdownTimeout != 20*time.Second {
		t.Fatalf("ShutdownTimeout = %s", cfg.ShutdownTimeout)
	}
}
