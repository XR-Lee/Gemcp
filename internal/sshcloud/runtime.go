package sshcloud

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var datasetEnvironmentVariable = regexp.MustCompile(`^GEMCP_DATASET_[A-Z0-9_]{1,112}$`)

type datasetEnv struct {
	Name  string
	Value string
}

type remoteWorkload struct {
	AssignmentID     string
	Image            string
	ExecutionMode    string
	Command          string
	Argv             []string
	WorkingDir       string
	GPUUUID          string
	CPULimit         int
	MemoryLimitBytes int64
	RemoteDir        string
	GraceSeconds     int
	Network          string
	DatasetEnv       []datasetEnv
}

type remoteState struct {
	Running    bool
	ExitCode   int
	OOMKilled  bool
	StartedAt  time.Time
	FinishedAt time.Time
}

func metricsRemote(ctx context.Context, conn Conn, remoteDir string) map[string]any {
	if !strings.HasPrefix(remoteDir, "/var/tmp/gemcp/") {
		return map[string]any{}
	}
	path := remoteDir + "/outputs/metrics.json"
	output, err := conn.Run(ctx, "umask 077; test -f "+shellQuote(path)+" && cat -- "+shellQuote(path)+" || true", maxMetricsBytes)
	if err != nil {
		return map[string]any{}
	}
	return parseMetricsJSON(output)
}

func parseMetricsJSON(raw string) map[string]any {
	payload := []byte(strings.TrimSpace(raw))
	if len(payload) == 0 || len(payload) > maxMetricsBytes || !utf8.Valid(payload) {
		return map[string]any{}
	}
	var result map[string]any
	if json.Unmarshal(payload, &result) != nil || result == nil {
		return map[string]any{}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxMetricsBytes {
		return map[string]any{}
	}
	return result
}

func mergeMetrics(existing, observed map[string]any) map[string]any {
	if len(observed) > 0 {
		return observed
	}
	if len(existing) > 0 {
		return existing
	}
	return map[string]any{}
}

func joinQuoted(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func snapshotDatasetEnv(snapshot map[string]any) []datasetEnv {
	raw, ok := snapshot["dataset_bindings"]
	if !ok || raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var bindings []struct {
		EnvironmentVariable string `json:"environment_variable"`
		CanonicalRoot       string `json:"canonical_root"`
	}
	if json.Unmarshal(encoded, &bindings) != nil {
		return nil
	}
	result := make([]datasetEnv, 0, len(bindings))
	for _, binding := range bindings {
		if env, ok := sanitizeDatasetEnv(binding.EnvironmentVariable, binding.CanonicalRoot); ok {
			result = append(result, env)
		}
	}
	return result
}

func sanitizeDatasetEnv(name, value string) (datasetEnv, bool) {
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if !datasetEnvironmentVariable.MatchString(name) {
		return datasetEnv{}, false
	}
	if value == "" || !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\x00\n\r") || len(value) > 1024 {
		return datasetEnv{}, false
	}
	return datasetEnv{Name: name, Value: value}, true
}
