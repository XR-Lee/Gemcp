package nodeagent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultConfigPath     = "/etc/gemcp-node/config.json"
	DefaultCredentialPath = "/etc/gemcp-node/credential"
	DefaultStatePath      = "/var/lib/gemcp-node/state.db"
	DefaultStorageRoot    = "/var/lib/gemcp-node/storage"
)

type Config struct {
	ServerURL          string `json:"server_url"`
	NodeID             string `json:"node_id"`
	InstallationID     string `json:"installation_id"`
	MachineFingerprint string `json:"machine_fingerprint"`
	CredentialPath     string `json:"credential_path"`
	StatePath          string `json:"state_path"`
	StorageRoot        string `json:"storage_root"`
}

func LoadConfig(filename string) (Config, error) {
	var config Config
	payload, err := os.ReadFile(filename)
	if err != nil {
		return config, fmt.Errorf("read node config: %w", err)
	}
	if err := json.Unmarshal(payload, &config); err != nil {
		return config, fmt.Errorf("decode node config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func SaveConfig(filename string, config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode node config: %w", err)
	}
	payload = append(payload, '\n')
	return atomicWrite(filename, payload, 0o600)
}

func LoadCredential(filename string) (string, error) {
	payload, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("read Node credential: %w", err)
	}
	token := strings.TrimSpace(string(payload))
	if !strings.HasPrefix(token, "gmn_") || len(token) < 32 {
		return "", fmt.Errorf("Node credential is invalid")
	}
	return token, nil
}

func SaveCredential(filename, token string) error {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "gmn_") || len(token) < 32 {
		return fmt.Errorf("Node credential is invalid")
	}
	return atomicWrite(filename, []byte(token+"\n"), 0o600)
}

func (c Config) Validate() error {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(c.ServerURL), "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Trim(parsed.Path, "/") != "" {
		return fmt.Errorf("node server URL must be a credential-free HTTPS origin")
	}
	for name, value := range map[string]string{
		"node_id": c.NodeID, "installation_id": c.InstallationID, "machine_fingerprint": c.MachineFingerprint,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("node config %s is required", name)
		}
	}
	for name, value := range map[string]string{
		"credential_path": c.CredentialPath, "state_path": c.StatePath, "storage_root": c.StorageRoot,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("node config %s must be an absolute path", name)
		}
	}
	if strings.ContainsAny(c.StorageRoot, ",\x00") {
		return fmt.Errorf("node config storage_root contains an unsupported character")
	}
	return nil
}

func atomicWrite(filename string, payload []byte, mode os.FileMode) error {
	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create node configuration directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".gemcp-node-*")
	if err != nil {
		return fmt.Errorf("create temporary node file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure temporary node file: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary node file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary node file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary node file: %w", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("install node file: %w", err)
	}
	return nil
}
