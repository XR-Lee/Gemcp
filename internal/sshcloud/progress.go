package sshcloud

import (
	"context"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
)

const (
	StepConnect       = "connect"
	StepOS            = "os"
	StepDocker        = "docker"
	StepDockerInstall = "docker_install"
	StepNvidiaDriver  = "nvidia_driver"
	StepNvidiaRuntime = "nvidia_runtime"
	StepNvidiaInstall = "nvidia_install"
	StepInventory     = "inventory"
	StepDone          = "done"
	StepFailed        = "failed"

	maxProbeSteps       = 48
	maxProbeOutput      = 8 << 10
	maxProbeOutputLines = 80
	probeFlushEvery     = 400 * time.Millisecond
)

type ProbeStep struct {
	At      string `json:"at"`
	Step    string `json:"step"`
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
	Status  string `json:"status"`
}

type probeLoggerKey struct{}
type streamOutputKey struct{}

func withProbeLogger(ctx context.Context, logger *probeLogger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, probeLoggerKey{}, logger)
}

func probeLoggerFrom(ctx context.Context) *probeLogger {
	logger, _ := ctx.Value(probeLoggerKey{}).(*probeLogger)
	return logger
}

func withStreamOutput(ctx context.Context) context.Context {
	return context.WithValue(ctx, streamOutputKey{}, true)
}

func streamRemoteOutput(ctx context.Context) bool {
	enabled, _ := ctx.Value(streamOutputKey{}).(bool)
	return enabled
}

type probeLogger struct {
	service   *Service
	record    *ent.CloudSSHNode
	secrets   []string
	mu        sync.Mutex
	steps     []ProbeStep
	lastFlush time.Time
}

func newProbeLogger(service *Service, record *ent.CloudSSHNode, secrets []string) *probeLogger {
	redacted := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if trimmed := strings.TrimSpace(secret); trimmed != "" {
			redacted = append(redacted, trimmed)
		}
	}
	return &probeLogger{service: service, record: record, secrets: redacted}
}

func (l *probeLogger) step(ctx context.Context, id, message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.finishLocked("ok")
	l.steps = append(l.steps, ProbeStep{
		At: l.now(), Step: id, Message: l.redact(message), Status: "running",
	})
	l.truncateLocked()
	l.mu.Unlock()
	l.flush(ctx, true)
}

func (l *probeLogger) output(ctx context.Context, chunk string) {
	if l == nil {
		return
	}
	text := l.redact(strings.TrimRight(chunk, "\r"))
	if strings.TrimSpace(text) == "" {
		return
	}
	l.mu.Lock()
	if len(l.steps) == 0 {
		l.steps = append(l.steps, ProbeStep{At: l.now(), Step: StepDockerInstall, Status: "running"})
	}
	current := &l.steps[len(l.steps)-1]
	current.Output = trimProbeOutput(current.Output + text)
	if !strings.HasSuffix(current.Output, "\n") && strings.Contains(text, "\n") {
		current.Output += "\n"
	}
	flushNow := time.Since(l.lastFlush) >= probeFlushEvery
	l.mu.Unlock()
	if flushNow {
		l.flush(ctx, false)
	}
}

func (l *probeLogger) fail(ctx context.Context, message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	if len(l.steps) == 0 || l.steps[len(l.steps)-1].Status != "running" {
		l.steps = append(l.steps, ProbeStep{At: l.now(), Step: StepFailed, Status: "failed"})
	}
	last := &l.steps[len(l.steps)-1]
	last.Status = "failed"
	if text := l.redact(strings.TrimSpace(message)); text != "" {
		if last.Message == "" {
			last.Message = text
		} else if !strings.Contains(last.Message, text) {
			last.Message = last.Message + " — " + text
		}
		last.Step = StepFailed
	}
	l.mu.Unlock()
	l.flush(ctx, true)
}

func (l *probeLogger) succeed(ctx context.Context, message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.finishLocked("ok")
	if strings.TrimSpace(message) != "" {
		l.steps = append(l.steps, ProbeStep{
			At: l.now(), Step: StepDone, Message: l.redact(message), Status: "ok",
		})
	}
	l.truncateLocked()
	l.mu.Unlock()
	l.flush(ctx, true)
}

func (l *probeLogger) snapshot() []ProbeStep {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]ProbeStep(nil), l.steps...)
}

func (l *probeLogger) finishLocked(status string) {
	if len(l.steps) == 0 {
		return
	}
	last := &l.steps[len(l.steps)-1]
	if last.Status == "running" {
		last.Status = status
	}
}

func (l *probeLogger) truncateLocked() {
	if len(l.steps) > maxProbeSteps {
		l.steps = append([]ProbeStep(nil), l.steps[len(l.steps)-maxProbeSteps:]...)
	}
}

func (l *probeLogger) now() string {
	if l.service != nil && l.service.now != nil {
		return l.service.now().UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}

func (l *probeLogger) redact(value string) string {
	text := value
	for _, secret := range l.secrets {
		text = strings.ReplaceAll(text, secret, "••••")
	}
	if strings.Contains(text, "PRIVATE KEY") {
		text = "[redacted private key material]"
	}
	return text
}

func (l *probeLogger) flush(ctx context.Context, force bool) {
	if l == nil || l.service == nil || l.record == nil {
		return
	}
	l.mu.Lock()
	if !force && time.Since(l.lastFlush) < probeFlushEvery {
		l.mu.Unlock()
		return
	}
	steps := append([]ProbeStep(nil), l.steps...)
	inventory := withProbeLog(l.record.Inventory, steps)
	l.lastFlush = time.Now()
	l.mu.Unlock()
	updated, err := l.record.Update().SetInventory(inventory).Save(ctx)
	if err != nil {
		return
	}
	l.mu.Lock()
	l.record = updated
	l.mu.Unlock()
}

func splitProbeLog(raw map[string]any) ([]ProbeStep, map[string]any) {
	if raw == nil {
		return nil, nil
	}
	inventory := maps.Clone(raw)
	steps := parseProbeLog(inventory["probe_log"])
	delete(inventory, "probe_log")
	if len(inventory) == 0 {
		return steps, nil
	}
	return steps, inventory
}

func withProbeLog(raw map[string]any, steps []ProbeStep) map[string]any {
	inventory := map[string]any{}
	if raw != nil {
		inventory = maps.Clone(raw)
	}
	if len(steps) == 0 {
		delete(inventory, "probe_log")
		return inventory
	}
	values := make([]any, 0, len(steps))
	for _, step := range steps {
		entry := map[string]any{"at": step.At, "step": step.Step, "message": step.Message, "status": step.Status}
		if step.Output != "" {
			entry["output"] = step.Output
		}
		values = append(values, entry)
	}
	inventory["probe_log"] = values
	return inventory
}

func parseProbeLog(raw any) []ProbeStep {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	steps := make([]ProbeStep, 0, len(values))
	for _, value := range values {
		entry, ok := value.(map[string]any)
		if !ok {
			continue
		}
		step := ProbeStep{
			At: stringValue(entry["at"]), Step: stringValue(entry["step"]), Message: stringValue(entry["message"]),
			Output: stringValue(entry["output"]), Status: stringValue(entry["status"]),
		}
		if step.Step == "" && step.Message == "" {
			continue
		}
		steps = append(steps, step)
	}
	return steps
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func trimProbeOutput(value string) string {
	text := value
	if len(text) > maxProbeOutput {
		text = text[len(text)-maxProbeOutput:]
		if index := strings.IndexByte(text, '\n'); index >= 0 && index+1 < len(text) {
			text = text[index+1:]
		}
	}
	lines := strings.Split(text, "\n")
	if len(lines) > maxProbeOutputLines {
		lines = lines[len(lines)-maxProbeOutputLines:]
	}
	return strings.Join(lines, "\n")
}
