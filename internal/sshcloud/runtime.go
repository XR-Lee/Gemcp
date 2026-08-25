package sshcloud

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

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
