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
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/XR-Lee/Gemcp/internal/server"
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
		return fmt.Errorf("watchdog is reserved for the M0 scheduler release")
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

	errCh := make(chan error, 1)
	go func() {
		slog.Info("control plane listening", "address", cfg.Address, "version", info.Version)
		errCh <- httpServer.ListenAndServe()
	}()

	stopCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case <-stopCtx.Done():
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancelShutdown()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve http: %w", err)
	}
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
