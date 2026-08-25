package sshcloud

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type sshConn struct {
	client *ssh.Client
}

func Dial(ctx context.Context, target Target, credential Credential, expectedFingerprint string) (Conn, string, error) {
	methods, err := authMethods(credential)
	if err != nil {
		return nil, "", err
	}
	var observed string
	config := &ssh.ClientConfig{
		User:              target.User,
		Auth:              methods,
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoRSA},
		Timeout:           20 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			observed = ssh.FingerprintSHA256(key)
			if expectedFingerprint == "" {
				return nil
			}
			if observed != expectedFingerprint {
				return ErrHostKeyChanged
			}
			return nil
		},
	}
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	network, err := dialer.DialContext(ctx, "tcp", target.Address())
	if err != nil {
		return nil, "", fmt.Errorf("connect Cloud SSH host: %w", err)
	}
	connection, channels, requests, err := ssh.NewClientConn(network, target.Address(), config)
	if err != nil {
		_ = network.Close()
		if errorsIsHostKey(err) {
			return nil, observed, ErrHostKeyChanged
		}
		return nil, observed, fmt.Errorf("authenticate Cloud SSH host: %w", err)
	}
	client := ssh.NewClient(connection, channels, requests)
	return &sshConn{client: client}, observed, nil
}

func (c *sshConn) Run(ctx context.Context, command string, maxOutput int) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("open Cloud SSH session: %w", err)
	}
	defer session.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = session.Setenv("GEMCP_DEADLINE", deadline.UTC().Format(time.RFC3339))
	}
	if streamRemoteOutput(ctx) {
		return runStreaming(ctx, session, command, maxOutput)
	}
	stdout := &limitedBuffer{maximum: maxOutput}
	stderr := &limitedBuffer{maximum: maxOutput}
	session.Stdout = stdout
	session.Stderr = stderr
	errCh := make(chan error, 1)
	go func() { errCh <- session.Run(command) }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return strings.TrimSpace(stdout.String()), ctx.Err()
	case err := <-errCh:
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
}

func runStreaming(ctx context.Context, session *ssh.Session, command string, maxOutput int) (string, error) {
	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("open Cloud SSH stdout: %w", err)
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("open Cloud SSH stderr: %w", err)
	}
	stdout := &limitedBuffer{maximum: maxOutput}
	stderr := &limitedBuffer{maximum: maxOutput}
	logger := probeLoggerFrom(ctx)
	if err := session.Start(command); err != nil {
		return "", fmt.Errorf("start Cloud SSH command: %w", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			copyProbeStream(ctx, stdoutPipe, stdout, logger)
		}()
		go func() {
			defer wg.Done()
			copyProbeStream(ctx, stderrPipe, stderr, logger)
		}()
		wg.Wait()
	}()
	errCh := make(chan error, 1)
	go func() { errCh <- session.Wait() }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		<-done
		return strings.TrimSpace(stdout.String()), ctx.Err()
	case err := <-errCh:
		<-done
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
}

func copyProbeStream(ctx context.Context, reader io.Reader, buffer *limitedBuffer, logger *probeLogger) {
	chunk := make([]byte, 2048)
	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			_, _ = buffer.Write(chunk[:n])
			if logger != nil {
				logger.output(ctx, string(chunk[:n]))
			}
		}
		if err != nil {
			return
		}
	}
}

func (c *sshConn) Upload(ctx context.Context, dest string, reader io.Reader) error {
	if strings.TrimSpace(dest) == "" || strings.ContainsAny(dest, "\x00") {
		return invalid("remote destination is invalid")
	}
	limited, err := limitSSHUpload(dest, reader)
	if err != nil {
		return err
	}
	reader = limited
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("open Cloud SSH upload session: %w", err)
	}
	defer session.Close()
	session.Stdin = reader
	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Run("umask 077; cat > " + shellQuote(dest))
	}()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return ctx.Err()
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("upload Cloud SSH file: %w", err)
		}
		return nil
	}
}

func (c *sshConn) Close() error {
	if c.client == nil {
		return nil
	}
	return c.client.Close()
}

const maxBootstrapUploadBytes = 64 << 10

func limitSSHUpload(dest string, reader io.Reader) (io.Reader, error) {
	if !restrictedSSHUpload(dest) {
		return reader, nil
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxBootstrapUploadBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBootstrapUploadBytes {
		return nil, invalid(fmt.Sprintf(
			"SSH upload %s is %d bytes; Cloud SSH refuses bootstrap payloads larger than %d bytes",
			dest, len(data), maxBootstrapUploadBytes,
		))
	}
	return bytes.NewReader(data), nil
}

func restrictedSSHUpload(dest string) bool {
	base := dest
	if index := strings.LastIndex(dest, "/"); index >= 0 {
		base = dest[index+1:]
	}
	lower := strings.ToLower(base)
	return strings.Contains(dest, "gemcp-bootstrap") || lower == "docker.tgz" || strings.HasPrefix(lower, "docker-") && strings.HasSuffix(lower, ".tgz")
}

func errorsIsHostKey(err error) bool {
	return err != nil && (strings.Contains(err.Error(), ErrHostKeyChanged.Error()) || strings.Contains(strings.ToLower(err.Error()), "host key"))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

type limitedBuffer struct {
	maximum int
	buffer  bytes.Buffer
}

func (w *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	if w.maximum <= 0 {
		return original, nil
	}
	if w.buffer.Len() >= w.maximum {
		return original, nil
	}
	remain := w.maximum - w.buffer.Len()
	if len(value) > remain {
		value = value[:remain]
	}
	_, _ = w.buffer.Write(value)
	return original, nil
}

func (w *limitedBuffer) String() string { return w.buffer.String() }
