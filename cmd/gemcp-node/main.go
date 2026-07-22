package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/XR-Lee/Gemcp/internal/nodeagent"
	"github.com/google/uuid"
)

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("gemcp-node stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "doctor":
		return runDoctor(args[1:])
	case "enroll":
		return runEnroll(args[1:])
	case "run":
		return runDaemon(args[1:])
	case "version":
		fmt.Printf("gemcp-node %s (%s, %s)\n", version, commit, builtAt)
		return nil
	default:
		return usageError()
	}
}

func runDoctor(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	storageRoot := flags.String("storage-root", nodeagent.DefaultStorageRoot, "managed storage root")
	jsonOutput := flags.Bool("json", false, "write JSON diagnostics")
	if err := flags.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := (nodeagent.SystemCollector{}).Collect(ctx, uuid.NewString(), *storageRoot, version)
	if *jsonOutput {
		encoded, encodeErr := json.MarshalIndent(report, "", "  ")
		if encodeErr != nil {
			return encodeErr
		}
		fmt.Println(string(encoded))
	} else {
		fmt.Printf("platform=%s/%s docker=%s gpus=%d storage=%s\n", report.OperatingSystem, report.Architecture, report.DockerVersion, len(report.Inventory.GPUs), *storageRoot)
	}
	if err != nil {
		return err
	}
	return nil
}

func runEnroll(args []string) error {
	flags := flag.NewFlagSet("enroll", flag.ContinueOnError)
	configPath := flags.String("config", nodeagent.DefaultConfigPath, "node config path")
	credentialPath := flags.String("credential", nodeagent.DefaultCredentialPath, "Node credential path")
	statePath := flags.String("state", nodeagent.DefaultStatePath, "bbolt state path")
	storageRoot := flags.String("storage-root", nodeagent.DefaultStorageRoot, "managed storage root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: gemcp-node enroll [flags] <setup-url>")
	}
	if _, err := os.Stat(*configPath); err == nil {
		return fmt.Errorf("node config already exists at %s", *configPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing node config: %w", err)
	}
	setupURL := flags.Arg(0)
	if setupURL == "-" {
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read node setup URL: %w", err)
			}
			return fmt.Errorf("node setup URL is required on standard input")
		}
		setupURL = strings.TrimSpace(scanner.Text())
	}
	origin, code, err := nodeagent.OriginFromSetupURL(setupURL)
	if err != nil {
		return err
	}
	installationID := uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	report, err := (nodeagent.SystemCollector{}).Collect(ctx, installationID, *storageRoot, version)
	if err != nil {
		return fmt.Errorf("node diagnostics failed: %w", err)
	}
	client, err := nodeagent.NewClient(origin, version)
	if err != nil {
		return err
	}
	claim, err := client.Claim(ctx, code, report.Inventory)
	if err != nil {
		return err
	}
	config := nodeagent.Config{
		ServerURL: origin, NodeID: claim.NodeID, InstallationID: installationID,
		MachineFingerprint: report.Inventory.MachineFingerprint, CredentialPath: filepath.Clean(*credentialPath),
		StatePath: filepath.Clean(*statePath), StorageRoot: filepath.Clean(*storageRoot),
	}
	if err := nodeagent.SaveCredential(config.CredentialPath, claim.NodeToken); err != nil {
		return err
	}
	if err := nodeagent.SaveConfig(filepath.Clean(*configPath), config); err != nil {
		return err
	}
	store, err := nodeagent.OpenStore(config.StatePath)
	if err != nil {
		return err
	}
	if err := store.Close(); err != nil {
		return err
	}
	fmt.Printf("GEMCP_NODE_ENROLLMENT_CLAIMED node_id=%s pairing_code=%s status=%s\n", claim.NodeID, claim.PairingCode, claim.Status)
	return nil
}

func runDaemon(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := flags.String("config", nodeagent.DefaultConfigPath, "node config path")
	logLevel := flags.String("log-level", "info", "debug, info, warn, or error")
	if err := flags.Parse(args); err != nil {
		return err
	}
	configureLogger(*logLevel)
	config, err := nodeagent.LoadConfig(filepath.Clean(*configPath))
	if err != nil {
		return err
	}
	token, err := nodeagent.LoadCredential(config.CredentialPath)
	if err != nil {
		return err
	}
	client, err := nodeagent.NewClient(config.ServerURL, version)
	if err != nil {
		return err
	}
	store, err := nodeagent.OpenStore(config.StatePath)
	if err != nil {
		return err
	}
	defer store.Close()
	agent, err := nodeagent.NewAgent(config, token, client, store, nodeagent.SystemCollector{}, version)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	slog.Info("gemcp-node started", "node_id", config.NodeID, "version", version)
	return agent.Run(ctx)
}

func configureLogger(raw string) {
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}

func usageError() error {
	return fmt.Errorf("usage: gemcp-node <doctor|enroll|run|version>")
}
