package sshcloud

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const localProcessFingerprint = "SHA256:local-process"

// LocalDial runs Cloud SSH observer commands on this machine instead of
// opening outbound SSH. It is used only when GEMCP_LOCAL_PROCESS_ENABLED is
// true and the registered host is loopback.
func LocalDial(_ context.Context, target Target, credential Credential, expected string) (Conn, string, error) {
	if !isLoopbackHost(target.Host) {
		return nil, "", invalid("local process overlay only accepts loopback hosts")
	}
	if _, err := normalizeCredential(credential); err != nil {
		return nil, "", err
	}
	if expected != "" && expected != localProcessFingerprint {
		return nil, localProcessFingerprint, ErrHostKeyChanged
	}
	return &localConn{}, localProcessFingerprint, nil
}

type localConn struct{}

func (c *localConn) Run(ctx context.Context, command string, maxOutput int) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", invalid("local process command is empty")
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	stdout := &limitedBuffer{maximum: maxOutput}
	stderr := &limitedBuffer{maximum: maxOutput}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())
	if err != nil {
		diagnostics := strings.TrimSpace(stderr.String())
		if diagnostics == "" {
			diagnostics = output
		}
		return output, fmt.Errorf("%w: %s", err, diagnostics)
	}
	return output, nil
}

func (c *localConn) Upload(ctx context.Context, dest string, reader io.Reader) error {
	if strings.TrimSpace(dest) == "" || strings.ContainsAny(dest, "\x00") {
		return invalid("remote destination is invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	limited, err := limitSSHUpload(dest, reader)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return fmt.Errorf("create local process destination: %w", err)
	}
	if err := os.WriteFile(dest, data, 0o600); err != nil {
		return fmt.Errorf("upload local process file: %w", err)
	}
	return nil
}

func (c *localConn) Close() error { return nil }
