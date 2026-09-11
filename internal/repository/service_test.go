package repository

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/XR-Lee/Gemcp/ent/enttest"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/ssh"
)

type fakeGitVerifier struct {
	accessErr   error
	commitErr   error
	publicErr   error
	publicOK    bool
	probedURL   string
	privateKey  []byte
	fingerprint string
	commitSHA   string
	ref         string
	resolvedSHA string
}

func (f *fakeGitVerifier) ProbePublicHTTPS(_ context.Context, httpsURL string) error {
	f.probedURL = httpsURL
	if f.publicOK {
		return nil
	}
	if f.publicErr != nil {
		return f.publicErr
	}
	return errors.New("not a public GitHub repository")
}

func (f *fakeGitVerifier) VerifyAccess(_ context.Context, _, _ string, privateKey []byte, fingerprint string) error {
	f.privateKey = append([]byte(nil), privateKey...)
	f.fingerprint = fingerprint
	return f.accessErr
}

func (f *fakeGitVerifier) VerifyCommit(_ context.Context, _, _ string, privateKey []byte, _ string, sha string) error {
	f.privateKey = append([]byte(nil), privateKey...)
	f.commitSHA = sha
	return f.commitErr
}

func (f *fakeGitVerifier) ResolveRef(_ context.Context, _, _ string, privateKey []byte, _ string, ref string) (string, error) {
	f.privateKey = append([]byte(nil), privateKey...)
	f.ref = ref
	if f.commitErr != nil {
		return "", f.commitErr
	}
	if f.resolvedSHA == "" {
		return strings.Repeat("a", 40), nil
	}
	return f.resolvedSHA, nil
}

func (f *fakeGitVerifier) DetectDefaultBranch(_ context.Context, _, _ string, privateKey []byte, _ string) (string, string, error) {
	f.privateKey = append([]byte(nil), privateKey...)
	if f.commitErr != nil {
		return "", "", f.commitErr
	}
	sha := f.resolvedSHA
	if sha == "" {
		sha = strings.Repeat("a", 40)
	}
	return "master", sha, nil
}

func TestBoundedFileWriterStopsAtLimit(t *testing.T) {
	path := t.TempDir() + "/archive.tar"
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer := &boundedFileWriter{file: file, remaining: 3}
	written, writeErr := writer.Write([]byte("abcd"))
	if closeErr := file.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if writeErr == nil || written != 3 || !writer.exceeded || info.Size() != 3 {
		t.Fatalf("written=%d err=%v exceeded=%t size=%d", written, writeErr, writer.exceeded, info.Size())
	}
}

func TestRepositoryDeployKeyLifecycle(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:repository?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Project").SetSlug("project").SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	verifier := &fakeGitVerifier{}
	service := NewService(client, box, verifier)

	created, err := service.Create(ctx, tenant.ID, Input{
		ProjectID: project.PublicID.String(), Name: "main", SSHURL: "git@github.com:XR-Lee/Gemcp.git", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != "pending_key" || !strings.HasPrefix(created.DeployPublicKey, "ssh-ed25519 ") {
		t.Fatalf("unexpected created repository: %+v", created)
	}
	if created.DeployKeySettingsURL != "https://github.com/XR-Lee/Gemcp/settings/keys" {
		t.Fatalf("deploy key settings URL = %q", created.DeployKeySettingsURL)
	}
	record, _ := client.Repository.Query().Only(ctx)
	if strings.Contains(record.DeployPrivateKeyCiphertext, "OPENSSH") {
		t.Fatal("deploy private key was stored as plaintext")
	}

	verified, err := service.Verify(ctx, tenant.ID, created.ID, "SHA256:expected-host-key-fingerprint")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verifier.fingerprint != "SHA256:expected-host-key-fingerprint" {
		t.Fatalf("Verify() fingerprint = %q", verifier.fingerprint)
	}
	if verified.Status != "active" || !strings.HasPrefix(verified.DeployPublicKey, "ssh-ed25519 ") || !strings.Contains(string(verifier.privateKey), "OPENSSH PRIVATE KEY") {
		t.Fatalf("unexpected verified repository: %+v", verified)
	}
	const sha = "0123456789012345678901234567890123456789"
	if err := service.VerifyCommit(ctx, record.ID, sha); err != nil {
		t.Fatalf("VerifyCommit() error = %v", err)
	}
	if verifier.commitSHA != sha {
		t.Fatalf("verified SHA = %q", verifier.commitSHA)
	}
	resolved, err := service.ResolveRef(ctx, record.ID, "main")
	if err != nil || resolved != strings.Repeat("a", 40) || verifier.ref != "main" {
		t.Fatalf("ResolveRef() = %q, ref=%q, err=%v", resolved, verifier.ref, err)
	}
	if _, err := service.ResolveRef(ctx, record.ID, "--upload-pack=evil"); err == nil {
		t.Fatal("ResolveRef() accepted an option-like ref")
	}
}

func TestPublicGitHubURLOnboarding(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:repository-public-url?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Project").SetSlug("project").SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	verifier := &fakeGitVerifier{publicOK: true}
	service := NewService(client, box, verifier)

	created, err := service.Create(ctx, tenant.ID, Input{
		ProjectID: project.PublicID.String(), URL: "https://github.com/octocat/Hello-World",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Name != "Hello-World" || created.SSHURL != "git@github.com:octocat/Hello-World.git" {
		t.Fatalf("normalized repository = %+v", created)
	}
	if created.Status != "active" || created.Access != AccessPublicHTTPS || created.DeployPublicKey != "" || created.DeployKeySettingsURL != "" {
		t.Fatalf("public repository = %+v", created)
	}
	if created.HostKeyFingerprint == nil || *created.HostKeyFingerprint != githubEd25519Fingerprint {
		t.Fatalf("public fingerprint = %+v", created.HostKeyFingerprint)
	}
	if verifier.probedURL != "https://github.com/octocat/Hello-World.git" {
		t.Fatalf("probed URL = %q", verifier.probedURL)
	}
	record, _ := client.Repository.Query().Only(ctx)
	if record.DeployPrivateKeyCiphertext != "" {
		t.Fatal("public registration stored a deploy key")
	}
}

func TestProbePublicHTTPSRejectsNonGitHub(t *testing.T) {
	if err := NewCommandVerifier().ProbePublicHTTPS(context.Background(), "https://evil.example/octocat/Hello-World.git"); err == nil {
		t.Fatal("ProbePublicHTTPS() accepted a non-GitHub URL")
	}
}

func TestParseGitHubRemote(t *testing.T) {
	remote, err := parseGitHubRemote("https://github.com/XR-Lee/Gemcp.git")
	if err != nil || remote.SSHURL != "git@github.com:XR-Lee/Gemcp.git" || remote.HTTPSURL != "https://github.com/XR-Lee/Gemcp.git" || remote.Name != "Gemcp" {
		t.Fatalf("https parse = %+v, %v", remote, err)
	}
	remote, err = parseGitHubRemote("git@github.com:XR-Lee/Gemcp")
	if err != nil || remote.SSHURL != "git@github.com:XR-Lee/Gemcp.git" {
		t.Fatalf("ssh parse = %+v, %v", remote, err)
	}
	if _, err := parseGitHubRemote("https://evil.example/XR-Lee/Gemcp"); err == nil {
		t.Fatal("accepted a non-GitHub URL")
	}
	if _, err := parseGitHubRemote("https://github.com/XR-Lee/Gemcp/issues/5"); err == nil {
		t.Fatal("accepted a GitHub URL with extra path")
	}
	if got := DeployKeySettingsURL("git@github.com:XR-Lee/Gemcp.git"); got != "https://github.com/XR-Lee/Gemcp/settings/keys" {
		t.Fatalf("DeployKeySettingsURL = %q", got)
	}
}

func TestParseSymrefHEAD(t *testing.T) {
	branch, sha, err := parseSymrefHEAD([]byte("ref: refs/heads/master\tHEAD\n0123456789abcdef0123456789abcdef01234567\tHEAD\n"))
	if err != nil || branch != "master" || sha != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("parseSymrefHEAD() = %q %q %v", branch, sha, err)
	}
	if _, _, err := parseSymrefHEAD([]byte("not a git ls-remote listing")); err == nil {
		t.Fatal("parseSymrefHEAD() accepted empty HEAD output")
	}
}

func TestRepositoryValidation(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:repository-validation?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	service := NewService(client, box, &fakeGitVerifier{})
	_, err := service.Create(context.Background(), 1, Input{ProjectID: "bad", Name: "repo", SSHURL: "ssh://localhost/repo"})
	if err == nil {
		t.Fatal("Create() accepted invalid project and SSH URL")
	}
}

func TestFilterPinnedHostKeys(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	otherPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	otherKey, _ := ssh.NewPublicKey(otherPublic)
	matchingLine := "github.com " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + "\n"
	otherLine := "github.com " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(otherKey))) + "\n"
	filtered, ok := filterPinnedHostKeys([]byte(otherLine+matchingLine), ssh.FingerprintSHA256(key))
	if !ok || string(filtered) != matchingLine {
		t.Fatalf("filterPinnedHostKeys() = %q, %v", filtered, ok)
	}
	if _, ok := filterPinnedHostKeys([]byte(matchingLine), "SHA256:wrong"); ok {
		t.Fatal("filterPinnedHostKeys() accepted wrong key")
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/tmp/key with'a quote"); got != "'/tmp/key with'\"'\"'a quote'" {
		t.Fatalf("shellQuote() = %q", got)
	}
}

func TestVerifyDoesNotActivateOnFailure(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:repository-failure?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Project").SetSlug("project").SetMonthlyBudgetMilli(100000).SetMaxExperimentMilli(20000).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	service := NewService(client, box, &fakeGitVerifier{accessErr: errors.New("access denied")})
	created, _ := service.Create(ctx, tenant.ID, Input{ProjectID: project.PublicID.String(), Name: "main", SSHURL: "git@github.com:XR-Lee/Gemcp.git"})
	if _, err := service.Verify(ctx, tenant.ID, created.ID, "SHA256:expected-host-key-fingerprint"); err == nil {
		t.Fatal("Verify() succeeded despite verifier failure")
	}
	record, _ := client.Repository.Query().Only(ctx)
	if record.Status != "pending_key" {
		t.Fatalf("repository status = %s", record.Status)
	}
}

func TestAgentRepositoryRegistrationIsProjectScopedAndAudited(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:repository-agent?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Project").SetSlug("project").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	other, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Other").SetSlug("other").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	verifier := &fakeGitVerifier{}
	service := NewService(client, box, verifier)
	pinned, err := service.Create(ctx, tenant.ID, Input{ProjectID: project.PublicID.String(), Name: "pinned", SSHURL: "git@github.com:XR-Lee/Gemcp.git"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(ctx, tenant.ID, pinned.ID, "SHA256:established-github-host-key"); err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateForAgent(ctx, tenant.ID, "agent-public-id", project.PublicID.String(), AgentInput{
		Name: "dynamic-point-mamba", SSHURL: "git@github.com:zhangtianyu00824/DynamicPointMamba.git", DefaultBranch: "main",
	})
	if err != nil || created.Status != "pending_key" || !strings.HasPrefix(created.DeployPublicKey, "ssh-ed25519 ") {
		t.Fatalf("CreateForAgent() = %+v, %v", created, err)
	}
	retried, err := service.CreateForAgent(ctx, tenant.ID, "agent-public-id", project.PublicID.String(), AgentInput{
		Name: "dynamic-point-mamba", SSHURL: "git@github.com:zhangtianyu00824/DynamicPointMamba.git", DefaultBranch: "main",
	})
	if err != nil || retried.ID != created.ID || retried.DeployPublicKey != created.DeployPublicKey {
		t.Fatalf("idempotent CreateForAgent() = %+v, %v", retried, err)
	}
	verified, err := service.VerifyForAgent(ctx, tenant.ID, "agent-public-id", project.PublicID.String(), AgentVerifyInput{RepositoryID: created.ID})
	if err != nil || verified.Status != "active" || verified.HostKeyFingerprint == nil || *verified.HostKeyFingerprint != "SHA256:established-github-host-key" {
		t.Fatalf("VerifyForAgent() = %+v, %v", verified, err)
	}
	otherRepository, err := service.Create(ctx, tenant.ID, Input{ProjectID: other.PublicID.String(), Name: "other", SSHURL: "git@github.com:owner/other.git"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyForAgent(ctx, tenant.ID, "agent-public-id", project.PublicID.String(), AgentVerifyInput{RepositoryID: otherRepository.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project VerifyForAgent() error = %v", err)
	}
	if audits, _ := client.AuditEvent.Query().Count(ctx); audits != 2 {
		t.Fatalf("agent audit count = %d", audits)
	}
}

func TestVerifyUsesOfficialGitHubFingerprintWhenOmitted(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:repository-official-pin?mode=memory&cache=shared&_fk=1")
	defer client.Close()
	ctx := context.Background()
	tenant, _ := client.Tenant.Create().SetName("Test").Save(ctx)
	project, _ := client.Project.Create().SetTenantID(tenant.ID).SetName("Project").SetSlug("project").SetMonthlyBudgetMilli(1).SetMaxExperimentMilli(1).Save(ctx)
	key, _ := secrets.GenerateMasterKey()
	box, _ := secrets.New(key)
	verifier := &fakeGitVerifier{}
	service := NewService(client, box, verifier)
	created, err := service.Create(ctx, tenant.ID, Input{ProjectID: project.PublicID.String(), Name: "main", SSHURL: "git@github.com:XR-Lee/DynamicPointMamba.git"})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := service.Verify(ctx, tenant.ID, created.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if verifier.fingerprint != githubEd25519Fingerprint {
		t.Fatalf("official fingerprint = %q", verifier.fingerprint)
	}
	if verified.Status != "active" || verified.HostKeyFingerprint == nil || *verified.HostKeyFingerprint != githubEd25519Fingerprint {
		t.Fatalf("verified repository = %+v", verified)
	}
}
