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
	if cfg.AutoMigrate {
		t.Fatal("AutoMigrate defaulted to true")
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
