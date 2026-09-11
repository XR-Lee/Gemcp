package repository

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
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

type Archive interface {
	Open() (io.ReadCloser, error)
	Close() error
	SizeBytes() int64
}

type SourceArchive struct {
	path    string
	tempDir string
	Size    int64
	RawSize int64
}

func (a *SourceArchive) Open() (io.ReadCloser, error) {
	if a == nil || a.path == "" {
		return nil, fmt.Errorf("source archive is closed")
	}
	return os.Open(a.path)
}

func (a *SourceArchive) SizeBytes() int64 {
	if a == nil {
		return 0
	}
	return a.Size
}

func (a *SourceArchive) Close() error {
	if a == nil || a.tempDir == "" {
		return nil
	}
	err := os.RemoveAll(a.tempDir)
	a.path = ""
	a.tempDir = ""
	return err
}

type cappedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

type boundedFileWriter struct {
	file      *os.File
	remaining int64
	exceeded  bool
}

func (w *boundedFileWriter) Write(value []byte) (int, error) {
	if int64(len(value)) > w.remaining {
		allowed := int(w.remaining)
		written := 0
		if allowed > 0 {
			written, _ = w.file.Write(value[:allowed])
			w.remaining -= int64(written)
		}
		w.exceeded = true
		return written, fmt.Errorf("source archive size limit exceeded")
	}
	written, err := w.file.Write(value)
	w.remaining -= int64(written)
	return written, err
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

func (v *CommandVerifier) ResolveRef(ctx context.Context, sshURL, host string, privateKey []byte, fingerprint, ref string) (string, error) {
	tempDir, repositoryPath, environment, err := v.fetchResolved(ctx, sshURL, host, privateKey, fingerprint, ref, false)
	if tempDir != "" {
		defer os.RemoveAll(tempDir)
	}
	if err != nil {
		return "", err
	}
	revParse := exec.CommandContext(ctx, v.gitBinary, "-C", repositoryPath, "rev-parse", "FETCH_HEAD")
	output, stderr, err := run(revParse, environment)
	if err != nil {
		return "", fmt.Errorf("resolve fetched commit: %w: %s", err, stderr)
	}
	commitSHA := strings.ToLower(strings.TrimSpace(string(output)))
	if len(commitSHA) != 40 && len(commitSHA) != 64 {
		return "", fmt.Errorf("resolved Git ref did not produce a full commit SHA")
	}
	return commitSHA, nil
}

func (v *CommandVerifier) ArchiveCommit(ctx context.Context, sshURL, host string, privateKey []byte, fingerprint, commitSHA string, maxBytes int64) (Archive, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("source archive size limit must be positive")
	}
	tempDir, repositoryPath, environment, err := v.fetch(ctx, sshURL, host, privateKey, fingerprint, commitSHA)
	if err != nil {
		return nil, err
	}
	_ = os.Remove(filepath.Join(tempDir, "deploy_key"))
	_ = os.Remove(filepath.Join(tempDir, "known_hosts"))
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(tempDir)
		}
	}()

	tarPath := filepath.Join(tempDir, "source.tar")
	archivePath := filepath.Join(tempDir, "source.tar.gz")
	tarFile, err := os.OpenFile(tarPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create source archive: %w", err)
	}
	writer := &boundedFileWriter{file: tarFile, remaining: maxBytes}
	stderr := &cappedBuffer{limit: maxCommandOutput}
	command := exec.CommandContext(ctx, v.gitBinary, "-C", repositoryPath, "archive", "--format=tar", "FETCH_HEAD")
	command.Env = environment
	command.Stdout = writer
	command.Stderr = stderr
	commandErr := command.Run()
	closeErr := tarFile.Close()
	if writer.exceeded {
		return nil, fmt.Errorf("source archive exceeds %d bytes", maxBytes)
	}
	if commandErr != nil {
		return nil, fmt.Errorf("archive requested Git commit: %w: %s", commandErr, strings.TrimSpace(stderr.String()))
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close source archive: %w", closeErr)
	}
	info, err := os.Stat(tarPath)
	if err != nil {
		return nil, fmt.Errorf("inspect source archive: %w", err)
	}
	if err := gzipFile(tarPath, archivePath); err != nil {
		return nil, err
	}
	compressed, err := os.Stat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("inspect compressed source archive: %w", err)
	}
	_ = os.Remove(tarPath)
	_ = os.RemoveAll(repositoryPath)
	cleanup = false
	return &SourceArchive{path: archivePath, tempDir: tempDir, Size: compressed.Size(), RawSize: info.Size()}, nil
}

func (v *CommandVerifier) verify(ctx context.Context, sshURL, host string, privateKey []byte, fingerprint, ref string) error {
	tempDir, _, _, err := v.fetch(ctx, sshURL, host, privateKey, fingerprint, ref)
	if tempDir != "" {
		defer os.RemoveAll(tempDir)
	}
	return err
}

func (v *CommandVerifier) fetch(ctx context.Context, sshURL, host string, privateKey []byte, fingerprint, ref string) (string, string, []string, error) {
	return v.fetchResolved(ctx, sshURL, host, privateKey, fingerprint, ref, true)
}

func (v *CommandVerifier) ProbePublicHTTPS(ctx context.Context, httpsURL string) error {
	httpsURL = strings.TrimSpace(httpsURL)
	if !strings.HasPrefix(httpsURL, "https://github.com/") {
		return fmt.Errorf("public HTTPS probe is limited to github.com")
	}
	environment := verificationEnvironment("")
	command := exec.CommandContext(ctx, v.gitBinary, "-c", "credential.helper=", "-c", "credential.interactive=never", "ls-remote", "--exit-code", httpsURL, "HEAD")
	if _, stderr, err := run(command, environment); err != nil {
		return fmt.Errorf("public GitHub HTTPS probe failed: %w: %s", err, stderr)
	}
	return nil
}

func (v *CommandVerifier) fetchResolved(ctx context.Context, sshURL, host string, privateKey []byte, fingerprint, ref string, requireExact bool) (string, string, []string, error) {
	if httpsURL := githubHTTPSURLFromSSH(sshURL); httpsURL != "" {
		tempDir, repositoryPath, environment, err := v.fetchHTTPS(ctx, httpsURL, ref, requireExact)
		if err == nil {
			return tempDir, repositoryPath, environment, nil
		}
		if len(privateKey) == 0 {
			return "", "", nil, err
		}
	} else if len(privateKey) == 0 {
		return "", "", nil, fmt.Errorf("repository fetch requires a deploy key or a public GitHub HTTPS URL")
	}

	tempDir, err := os.MkdirTemp("", "gemcp-git-fetch-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create Git workspace: %w", err)
	}
	fail := func(err error) (string, string, []string, error) {
		_ = os.RemoveAll(tempDir)
		return "", "", nil, err
	}

	keyPath := filepath.Join(tempDir, "deploy_key")
	knownHostsPath := filepath.Join(tempDir, "known_hosts")
	repositoryPath := filepath.Join(tempDir, "repository.git")
	if err := os.WriteFile(keyPath, privateKey, 0o600); err != nil {
		return fail(fmt.Errorf("write deploy key: %w", err))
	}

	scan := exec.CommandContext(ctx, v.sshKeyscanBinary, "-T", "10", "-t", "ed25519,ecdsa,rsa", host)
	hostKeys, scanStderr, err := run(scan, verificationEnvironment(tempDir))
	if err != nil || len(hostKeys) == 0 {
		return fail(fmt.Errorf("scan SSH host key: %w: %s", err, scanStderr))
	}
	pinnedHostKeys, ok := filterPinnedHostKeys(hostKeys, fingerprint)
	if !ok {
		return fail(fmt.Errorf("SSH host key fingerprint did not match the pinned value"))
	}
	if err := os.WriteFile(knownHostsPath, pinnedHostKeys, 0o600); err != nil {
		return fail(fmt.Errorf("write known hosts: %w", err))
	}
	environment := verificationEnvironment(tempDir)
	environment = append(environment, "GIT_SSH_COMMAND=ssh -F /dev/null -i "+shellQuote(keyPath)+
		" -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o UserKnownHostsFile="+shellQuote(knownHostsPath)+
		" -o BatchMode=yes -o ConnectTimeout=15")
	initCommand := exec.CommandContext(ctx, v.gitBinary, "init", "--bare", repositoryPath)
	if _, stderr, err := run(initCommand, environment); err != nil {
		return fail(fmt.Errorf("initialize Git repository: %w: %s", err, stderr))
	}
	fetchCommand := exec.CommandContext(ctx, v.gitBinary, "-C", repositoryPath, "fetch", "--depth=1", "--no-tags", sshURL, ref)
	if _, stderr, err := run(fetchCommand, environment); err != nil {
		return fail(fmt.Errorf("fetch requested Git ref: %w: %s", err, stderr))
	}
	if requireExact && ref != "HEAD" {
		revParse := exec.CommandContext(ctx, v.gitBinary, "-C", repositoryPath, "rev-parse", "FETCH_HEAD")
		output, stderr, err := run(revParse, environment)
		if err != nil {
			return fail(fmt.Errorf("resolve fetched commit: %w: %s", err, stderr))
		}
		if !strings.EqualFold(strings.TrimSpace(string(output)), strings.TrimSpace(ref)) {
			return fail(fmt.Errorf("fetched commit did not match the requested SHA"))
		}
	}
	return tempDir, repositoryPath, environment, nil
}

func (v *CommandVerifier) fetchHTTPS(ctx context.Context, httpsURL, ref string, requireExact bool) (string, string, []string, error) {
	tempDir, err := os.MkdirTemp("", "gemcp-git-https-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create Git workspace: %w", err)
	}
	fail := func(err error) (string, string, []string, error) {
		_ = os.RemoveAll(tempDir)
		return "", "", nil, err
	}
	repositoryPath := filepath.Join(tempDir, "repository.git")
	environment := verificationEnvironment(tempDir)
	initCommand := exec.CommandContext(ctx, v.gitBinary, "init", "--bare", repositoryPath)
	if _, stderr, err := run(initCommand, environment); err != nil {
		return fail(fmt.Errorf("initialize Git repository: %w: %s", err, stderr))
	}
	fetchCommand := exec.CommandContext(ctx, v.gitBinary, "-C", repositoryPath, "-c", "credential.helper=", "-c", "credential.interactive=never", "fetch", "--depth=1", "--no-tags", httpsURL, ref)
	if _, stderr, err := run(fetchCommand, environment); err != nil {
		return fail(fmt.Errorf("fetch public GitHub HTTPS ref: %w: %s", err, stderr))
	}
	if requireExact && ref != "HEAD" {
		revParse := exec.CommandContext(ctx, v.gitBinary, "-C", repositoryPath, "rev-parse", "FETCH_HEAD")
		output, stderr, err := run(revParse, environment)
		if err != nil {
			return fail(fmt.Errorf("resolve fetched commit: %w: %s", err, stderr))
		}
		if !strings.EqualFold(strings.TrimSpace(string(output)), strings.TrimSpace(ref)) {
			return fail(fmt.Errorf("fetched commit did not match the requested SHA"))
		}
	}
	return tempDir, repositoryPath, environment, nil
}

func gzipFile(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source archive: %w", err)
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create compressed source archive: %w", err)
	}
	writer := gzip.NewWriter(destination)
	_, copyErr := io.Copy(writer, source)
	gzipErr := writer.Close()
	fileErr := destination.Close()
	if copyErr != nil {
		return fmt.Errorf("compress source archive: %w", copyErr)
	}
	if gzipErr != nil {
		return fmt.Errorf("finish source compression: %w", gzipErr)
	}
	if fileErr != nil {
		return fmt.Errorf("close compressed source archive: %w", fileErr)
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
	if home == "" {
		home = os.TempDir()
	}
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"LANG=C",
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=true",
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
