package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/XR-Lee/Gemcp/internal/buildinfo"
	"github.com/XR-Lee/Gemcp/internal/config"
	"github.com/XR-Lee/Gemcp/internal/database"
	"github.com/XR-Lee/Gemcp/internal/execution"
	"github.com/XR-Lee/Gemcp/internal/notification"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/server"
	"github.com/XR-Lee/Gemcp/internal/watchdog"
)

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("gemcp stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "serve":
		return runServer()
	case "watchdog":
		return runWatchdog()
	case "phase0":
		return runPhaseZero(args[1:])
	case "keygen":
		key, err := secrets.GenerateMasterKey()
		if err != nil {
			return err
		}
		fmt.Println(key)
		return nil
	case "bootstrap-token":
		token, _, err := secrets.RandomToken("gmb", 32)
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	case "version":
		info := buildinfo.New(version, commit, builtAt)
		fmt.Printf("%s %s (%s, %s)\n", info.Name, info.Version, info.Commit, info.BuiltAt)
		return nil
	default:
		return fmt.Errorf("unknown command %q (expected serve, watchdog, phase0, keygen, bootstrap-token, or version)", command)
	}
}

func runServer() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	configureLogger(cfg.LogLevel)
	secretBox, err := secrets.New(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("load master key: %w", err)
	}

	connectCtx, cancelConnect := context.WithTimeout(context.Background(), cfg.DatabaseConnectTimeout)
	defer cancelConnect()
	store, err := database.Open(connectCtx, cfg.DatabaseURL, cfg.AutoMigrate)
	if err != nil {
		return err
	}
	defer store.Close()

	info := buildinfo.New(version, commit, builtAt)
	httpServer := server.New(server.Dependencies{Config: cfg, Build: info, DB: store, Ent: store.Client, Secrets: secretBox})
	provider := execution.NewProvider(secretBox, info.Version)
	executionConfig := execution.DefaultConfig()
	executionConfig.Enabled = cfg.SchedulerEnabled
	executionConfig.PublicURL = cfg.PublicURL
	executionConfig.InstanceID = processID("controlplane")
	executionConfig.GlobalConcurrency = cfg.GlobalConcurrency
	executionConfig.PollInterval = cfg.SchedulerPollInterval
	executionConfig.LeaseDuration = cfg.ExecutionLeaseDuration
	executionConfig.ProvisionTimeout = cfg.ProvisionTimeout
	executionConfig.ReconcileDelay = cfg.ReconcileDelay
	executionConfig.CallbackGrace = cfg.CallbackGrace
	executionConfig.MaxAttempts = cfg.MaxAttempts
	engine, err := execution.NewEngine(store.Client, secretBox, provider, executionConfig)
	if err != nil {
		return fmt.Errorf("initialize execution scheduler: %w", err)
	}

	processCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	notificationWorker := notification.NewWorker(store.Client, secretBox, nil, cfg.NotificationPollInterval, notification.WithWorkerInstanceID(processID("notification")))
	go func() {
		slog.Info("notification worker started")
		_ = notificationWorker.Run(processCtx)
	}()
	go func() {
		slog.Info("execution reconciler started", "instance", executionConfig.InstanceID, "dispatch_enabled", cfg.SchedulerEnabled, "global_concurrency", cfg.GlobalConcurrency)
		_ = engine.Run(processCtx)
	}()
	if !cfg.SchedulerEnabled {
		slog.Info("execution dispatch disabled; queued experiments will not run")
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("control plane listening", "address", cfg.Address, "version", info.Version)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-processCtx.Done():
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancelShutdown()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		return nil
	case err := <-errCh:
		stop()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve http: %w", err)
	}
}

func runWatchdog() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	configureLogger(cfg.LogLevel)
	secretBox, err := secrets.New(cfg.MasterKey)
	if err != nil {
		return fmt.Errorf("load master key: %w", err)
	}
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), cfg.DatabaseConnectTimeout)
	defer cancelConnect()
	store, err := database.Open(connectCtx, cfg.DatabaseURL, false)
	if err != nil {
		return err
	}
	defer store.Close()

	watchdogConfig := watchdog.DefaultConfig()
	watchdogConfig.InstanceID = processID("watchdog")
	watchdogConfig.PollInterval = cfg.WatchdogPollInterval
	watchdogConfig.LeaseDuration = cfg.WatchdogLeaseDuration
	watchdogConfig.ReconcileDelay = cfg.ReconcileDelay
	watchdogConfig.BatchSize = cfg.WatchdogBatchSize
	service, err := watchdog.New(store.Client, execution.NewProvider(secretBox, version), watchdogConfig)
	if err != nil {
		return fmt.Errorf("initialize watchdog: %w", err)
	}
	processCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	slog.Info("watchdog started", "instance", watchdogConfig.InstanceID, "version", version)
	return service.Run(processCtx)
}

func processID(role string) string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%s-%d", role, hostname, os.Getpid())
}

func configureLogger(level string) {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})))
}
