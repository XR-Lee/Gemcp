package repository

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const maxCommandOutput = 64 << 10

type CommandVerifier struct {
	gitBinary        string
	sshKeyscanBinary string
}

type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.buffer.Write(value)
	}
	return originalLength, nil
}

func (b *cappedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *cappedBuffer) String() string { return b.buffer.String() }

func NewCommandVerifier() *CommandVerifier {
	return &CommandVerifier{gitBinary: "git", sshKeyscanBinary: "ssh-keyscan"}
}

func (v *CommandVerifier) VerifyAccess(ctx context.Context, sshURL, host string, privateKey []byte, fingerprint string) error {
	return v.verify(ctx, sshURL, host, privateKey, fingerprint, "HEAD")
}

func (v *CommandVerifier) VerifyCommit(ctx context.Context, sshURL, host string, privateKey []byte, fingerprint, commitSHA string) error {
	return v.verify(ctx, sshURL, host, privateKey, fingerprint, commitSHA)
}

func (v *CommandVerifier) verify(ctx context.Context, sshURL, host string, privateKey []byte, fingerprint, ref string) error {
	tempDir, err := os.MkdirTemp("", "gemcp-git-verify-*")
	if err != nil {
		return fmt.Errorf("create verification workspace: %w", err)
	}
	defer os.RemoveAll(tempDir)
	keyPath := filepath.Join(tempDir, "deploy_key")
	knownHostsPath := filepath.Join(tempDir, "known_hosts")
	repositoryPath := filepath.Join(tempDir, "repository.git")
	if err := os.WriteFile(keyPath, privateKey, 0o600); err != nil {
		return fmt.Errorf("write deploy key: %w", err)
	}

	scan := exec.CommandContext(ctx, v.sshKeyscanBinary, "-T", "10", "-t", "ed25519,ecdsa,rsa", host)
	hostKeys, scanStderr, err := run(scan, verificationEnvironment(tempDir))
	if err != nil || len(hostKeys) == 0 {
		return fmt.Errorf("scan SSH host key: %w: %s", err, scanStderr)
	}
	pinnedHostKeys, ok := filterPinnedHostKeys(hostKeys, fingerprint)
	if !ok {
		return fmt.Errorf("SSH host key fingerprint did not match the pinned value")
	}
	if err := os.WriteFile(knownHostsPath, pinnedHostKeys, 0o600); err != nil {
		return fmt.Errorf("write known hosts: %w", err)
	}
	environment := verificationEnvironment(tempDir)
	environment = append(environment, "GIT_SSH_COMMAND=ssh -F /dev/null -i "+shellQuote(keyPath)+
		" -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile="+shellQuote(knownHostsPath)+
		" -o BatchMode=yes -o ConnectTimeout=15")
	initCommand := exec.CommandContext(ctx, v.gitBinary, "init", "--bare", repositoryPath)
	if _, stderr, err := run(initCommand, environment); err != nil {
		return fmt.Errorf("initialize verification repository: %w: %s", err, stderr)
	}
	fetchCommand := exec.CommandContext(ctx, v.gitBinary, "-C", repositoryPath, "fetch", "--depth=1", "--no-tags", sshURL, ref)
	if _, stderr, err := run(fetchCommand, environment); err != nil {
		return fmt.Errorf("fetch requested Git ref: %w: %s", err, stderr)
	}
	if ref == "HEAD" {
		return nil
	}
	revParse := exec.CommandContext(ctx, v.gitBinary, "-C", repositoryPath, "rev-parse", "FETCH_HEAD")
	output, stderr, err := run(revParse, environment)
	if err != nil {
		return fmt.Errorf("resolve fetched commit: %w: %s", err, stderr)
	}
	if !strings.EqualFold(strings.TrimSpace(string(output)), strings.TrimSpace(ref)) {
		return fmt.Errorf("fetched commit did not match the requested SHA")
	}
	return nil
}

func filterPinnedHostKeys(knownHosts []byte, expected string) ([]byte, bool) {
	var pinned bytes.Buffer
	rest := knownHosts
	for len(rest) > 0 {
		original := rest
		_, _, key, _, next, err := ssh.ParseKnownHosts(rest)
		if err != nil {
			lineEnd := bytes.IndexByte(rest, '\n')
			if lineEnd < 0 {
				break
			}
			rest = rest[lineEnd+1:]
			continue
		}
		if ssh.FingerprintSHA256(key) == expected {
			consumed := len(original) - len(next)
			pinned.Write(original[:consumed])
			if consumed == 0 || original[consumed-1] != '\n' {
				pinned.WriteByte('\n')
			}
		}
		rest = next
	}
	return pinned.Bytes(), pinned.Len() > 0
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func verificationEnvironment(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"LANG=C",
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"SSH_AUTH_SOCK=",
	}
}

func run(command *exec.Cmd, environment []string) ([]byte, string, error) {
	stdout := &cappedBuffer{limit: maxCommandOutput}
	stderr := &cappedBuffer{limit: maxCommandOutput}
	command.Env = environment
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	return stdout.Bytes(), strings.TrimSpace(stderr.String()), err
}
