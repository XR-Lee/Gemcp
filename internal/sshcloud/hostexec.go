package sshcloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/XR-Lee/Gemcp/internal/executioncmd"
)

func startHostProcess(ctx context.Context, conn Conn, spec remoteWorkload) (string, error) {
	if err := validateHostSpec(spec); err != nil {
		return "", err
	}
	outputDir := spec.RemoteDir + "/outputs"
	if _, err := conn.Run(ctx, "umask 077; mkdir -p "+shellQuote(outputDir), 4<<10); err != nil {
		return "", fmt.Errorf("create observer output directory: %w", err)
	}
	output, err := conn.Run(ctx, "sh -lc "+shellQuote(hostStartScript(spec)), 8<<10)
	if err != nil {
		return "", fmt.Errorf("start host process: %w", err)
	}
	pid := parseHostPID(output)
	if pid == "" {
		return "", fmt.Errorf("host process started without a pid")
	}
	return pid, nil
}

func inspectHostProcess(ctx context.Context, conn Conn, spec remoteWorkload, pid string) (remoteState, error) {
	var result remoteState
	output, err := conn.Run(ctx, "sh -lc "+shellQuote(hostStatusScript(spec, pid)), 4<<10)
	if err != nil {
		return result, err
	}
	return parseHostStatus(output), nil
}

func logsHostProcess(ctx context.Context, conn Conn, spec remoteWorkload) (string, error) {
	output, err := conn.Run(ctx, "sh -lc "+shellQuote("echo gemcp-host-logs; tail -c "+strconv.Itoa(maxLogTailBytes)+" "+shellQuote(spec.RemoteDir+"/log")+" 2>/dev/null || true"), maxLogTailBytes+64)
	if err != nil {
		return "", err
	}
	return trimHostMarker(output, "gemcp-host-logs"), nil
}

func stopHostProcess(ctx context.Context, conn Conn, spec remoteWorkload, pid string, graceSeconds int) error {
	if graceSeconds < 0 || graceSeconds > 3600 {
		graceSeconds = 5
	}
	_, err := conn.Run(ctx, "sh -lc "+shellQuote(hostStopScript(spec, pid, graceSeconds)), 8<<10)
	return err
}

func cleanupHostProcess(ctx context.Context, conn Conn, spec remoteWorkload, pid string) {
	if pid != "" {
		_, _ = conn.Run(ctx, "sh -lc "+shellQuote(hostStopScript(spec, pid, 0)), 4<<10)
	}
	if strings.HasPrefix(spec.RemoteDir, "/var/tmp/gemcp/") {
		_, _ = conn.Run(ctx, "rm -rf "+shellQuote(spec.RemoteDir), 4<<10)
	}
}

func validateHostSpec(spec remoteWorkload) error {
	if _, err := executioncmd.Validate(executioncmd.Spec{Mode: spec.ExecutionMode, Command: spec.Command, Argv: spec.Argv}); err != nil {
		return fmt.Errorf("workload execution specification is invalid: %w", err)
	}
	if len(strings.ReplaceAll(spec.AssignmentID, "-", "")) < 16 || !strings.HasPrefix(spec.RemoteDir, "/var/tmp/gemcp/") || strings.ContainsAny(spec.RemoteDir, ",\x00") {
		return fmt.Errorf("host observer specification is invalid")
	}
	if spec.WorkingDir != "" && (!strings.HasPrefix(spec.WorkingDir, "/") || strings.ContainsAny(spec.WorkingDir, "\x00")) {
		return fmt.Errorf("working directory must be an absolute path")
	}
	return nil
}

func hostStartScript(spec remoteWorkload) string {
	cwd := spec.WorkingDir
	command := hostCommand(spec)
	return `
echo gemcp-host-start
dir=` + shellQuote(spec.RemoteDir) + `
umask 077
mkdir -p "$dir/outputs"
if [ -n ` + shellQuote(cwd) + ` ]; then
  cd ` + shellQuote(cwd) + ` || exit 127
else
  cd "${HOME:-/}" || exit 127
fi
export GEMCP_OUTPUT_DIR="$dir/outputs"
if command -v setsid >/dev/null 2>&1; then
  setsid sh -lc ` + shellQuote(command+`; echo $? > "$dir/exit"`) + ` </dev/null >"$dir/log" 2>&1 &
else
  nohup sh -lc ` + shellQuote(command+`; echo $? > "$dir/exit"`) + ` </dev/null >"$dir/log" 2>&1 &
fi
printf '%s\n' "$!" >"$dir/pid"
printf 'PID %s\n' "$(tr -d ' \n' < "$dir/pid")"
`
}

func hostStatusScript(spec remoteWorkload, pid string) string {
	return `
echo gemcp-host-status
dir=` + shellQuote(spec.RemoteDir) + `
pid=$(tr -d ' \n' < "$dir/pid" 2>/dev/null || true)
if [ -z "$pid" ]; then
  pid=` + shellQuote(pid) + `
fi
if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
  printf 'RUNNING %s\n' "$pid"
else
  code=$(tr -d ' \n' < "$dir/exit" 2>/dev/null || echo 1)
  printf 'STOPPED %s\n' "$code"
fi
`
}

func hostStopScript(spec remoteWorkload, pid string, graceSeconds int) string {
	return `
echo gemcp-host-stop
dir=` + shellQuote(spec.RemoteDir) + `
pid=$(tr -d ' \n' < "$dir/pid" 2>/dev/null || true)
if [ -z "$pid" ]; then
  pid=` + shellQuote(pid) + `
fi
if [ -z "$pid" ]; then
  exit 0
fi
if kill -0 "$pid" 2>/dev/null; then
  kill -TERM -"$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
  i=0
  while [ "$i" -lt ` + strconv.Itoa(graceSeconds) + ` ] && kill -0 "$pid" 2>/dev/null; do
    sleep 1
    i=$((i+1))
  done
  kill -KILL -"$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
fi
`
}

func hostCommand(spec remoteWorkload) string {
	if spec.ExecutionMode == executioncmd.ModeArgv {
		return joinQuoted(spec.Argv)
	}
	return spec.Command
}

func parseHostPID(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "PID "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

func parseHostStatus(raw string) remoteState {
	var result remoteState
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "RUNNING":
			result.Running = true
			return result
		case "STOPPED":
			result.ExitCode, _ = strconv.Atoi(fields[1])
			return result
		}
	}
	result.ExitCode = 1
	return result
}

func trimHostMarker(raw, marker string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == marker {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return raw
}
