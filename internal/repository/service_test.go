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
	accessErr  error
	commitErr  error
	privateKey []byte
	commitSHA  string
}

func (f *fakeGitVerifier) VerifyAccess(_ context.Context, _, _ string, privateKey []byte, _ string) error {
	f.privateKey = append([]byte(nil), privateKey...)
	return f.accessErr
}

func (f *fakeGitVerifier) VerifyCommit(_ context.Context, _, _ string, privateKey []byte, _ string, sha string) error {
	f.privateKey = append([]byte(nil), privateKey...)
	f.commitSHA = sha
	return f.commitErr
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
	record, _ := client.Repository.Query().Only(ctx)
	if strings.Contains(record.DeployPrivateKeyCiphertext, "OPENSSH") {
		t.Fatal("deploy private key was stored as plaintext")
	}

	verified, err := service.Verify(ctx, tenant.ID, created.ID, "SHA256:expected-host-key-fingerprint")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
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
