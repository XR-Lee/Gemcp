package repository

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	entproject "github.com/XR-Lee/Gemcp/ent/project"
	entrepository "github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

var (
	ErrNotFound           = errors.New("repository not found")
	ErrNotActive          = errors.New("repository is not active")
	ErrVerificationFailed = errors.New("repository verification failed")
	githubSSHURL          = regexp.MustCompile(`^git@github\.com:([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?$`)
	branchPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
)

const deployKeyAADPrefix = "gemcp:repository-deploy-key:v1:"

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

type Input struct {
	ProjectID     string `json:"project_id"`
	Name          string `json:"name"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
}

type View struct {
	ID                 string     `json:"id"`
	ProjectID          string     `json:"project_id"`
	Name               string     `json:"name"`
	SSHURL             string     `json:"ssh_url"`
	DefaultBranch      string     `json:"default_branch"`
	Status             string     `json:"status"`
	DeployPublicKey    string     `json:"deploy_public_key,omitempty"`
	HostKeyFingerprint *string    `json:"host_key_fingerprint,omitempty"`
	LastVerifiedAt     *time.Time `json:"last_verified_at,omitempty"`
}

type GitVerifier interface {
	VerifyAccess(context.Context, string, string, []byte, string) error
	VerifyCommit(context.Context, string, string, []byte, string, string) error
}

type GitArchiver interface {
	ArchiveCommit(context.Context, string, string, []byte, string, string, int64) (Archive, error)
}

type Service struct {
	client   *ent.Client
	box      *secrets.Box
	verifier GitVerifier
	now      func() time.Time
}

func NewService(client *ent.Client, box *secrets.Box, verifier GitVerifier) *Service {
	if verifier == nil {
		verifier = NewCommandVerifier()
	}
	return &Service{client: client, box: box, verifier: verifier, now: time.Now}
}

func (s *Service) Create(ctx context.Context, tenantID int, input Input) (View, error) {
	var view View
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return view, invalid("valid project_id is required")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 120 {
		return view, invalid("repository name is required")
	}
	sshURL := strings.TrimSpace(input.SSHURL)
	if !githubSSHURL.MatchString(sshURL) {
		return view, invalid("ssh_url must match git@github.com:owner/repository.git")
	}
	branch := strings.TrimSpace(input.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	if !branchPattern.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "//") {
		return view, invalid("default branch is invalid")
	}
	project, err := s.client.Project.Query().
		Where(entproject.PublicIDEQ(projectID), entproject.TenantIDEQ(tenantID), entproject.StatusEQ("active")).
		Only(ctx)
	if ent.IsNotFound(err) {
		return view, ErrNotFound
	}
	if err != nil {
		return view, err
	}
	publicID := uuid.New()
	publicKey, privateKey, err := generateDeployKey("gemcp-" + publicID.String())
	if err != nil {
		return view, err
	}
	ciphertext, err := s.box.Encrypt(privateKey, deployKeyAADPrefix+publicID.String())
	if err != nil {
		return view, err
	}
	record, err := s.client.Repository.Create().
		SetPublicID(publicID).
		SetProjectID(project.ID).
		SetName(name).
		SetSSHURL(sshURL).
		SetSSHHost("github.com").
		SetDefaultBranch(branch).
		SetDeployPublicKey(publicKey).
		SetDeployPrivateKeyCiphertext(ciphertext).
		Save(ctx)
	if err != nil {
		return view, err
	}
	return makeView(record, project.PublicID.String(), true), nil
}

func (s *Service) Verify(ctx context.Context, tenantID int, publicID, fingerprint string) (View, error) {
	var view View
	record, projectID, err := s.findForTenant(ctx, tenantID, publicID)
	if err != nil {
		return view, err
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if !strings.HasPrefix(fingerprint, "SHA256:") || len(fingerprint) < 20 || len(fingerprint) > 100 {
		return view, invalid("valid SHA256 host key fingerprint is required")
	}
	privateKey, err := s.decryptKey(record)
	if err != nil {
		return view, err
	}
	defer wipe(privateKey)
	if err := s.verifier.VerifyAccess(ctx, record.SSHURL, record.SSHHost, privateKey, fingerprint); err != nil {
		return view, fmt.Errorf("%w: %v", ErrVerificationFailed, err)
	}
	now := s.now().UTC()
	record, err = record.Update().
		SetHostKeyFingerprint(fingerprint).
		SetStatus("active").
		SetLastVerifiedAt(now).
		Save(ctx)
	if err != nil {
		return view, err
	}
	return makeView(record, projectID, true), nil
}

func (s *Service) List(ctx context.Context, tenantID int, projectPublicID string) ([]View, error) {
	projectID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return nil, invalid("valid project_id is required")
	}
	records, err := s.client.Repository.Query().
		Where(entrepository.HasProjectWith(entproject.PublicIDEQ(projectID), entproject.TenantIDEQ(tenantID))).
		WithProject().
		Order(ent.Asc(entrepository.FieldName)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]View, 0, len(records))
	for _, record := range records {
		views = append(views, makeView(record, record.Edges.Project.PublicID.String(), true))
	}
	return views, nil
}

func (s *Service) VerifyCommit(ctx context.Context, repositoryID int, commitSHA string) error {
	record, err := s.client.Repository.Get(ctx, repositoryID)
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if record.Status != "active" || record.HostKeyFingerprint == "" {
		return ErrNotActive
	}
	privateKey, err := s.decryptKey(record)
	if err != nil {
		return err
	}
	defer wipe(privateKey)
	return s.verifier.VerifyCommit(ctx, record.SSHURL, record.SSHHost, privateKey, record.HostKeyFingerprint, commitSHA)
}

func (s *Service) ArchiveCommit(ctx context.Context, repositoryID int, commitSHA string, maxBytes int64) (Archive, error) {
	record, err := s.client.Repository.Get(ctx, repositoryID)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if record.Status != "active" || record.HostKeyFingerprint == "" {
		return nil, ErrNotActive
	}
	archiver, ok := s.verifier.(GitArchiver)
	if !ok {
		return nil, fmt.Errorf("repository archiver is unavailable")
	}
	privateKey, err := s.decryptKey(record)
	if err != nil {
		return nil, err
	}
	defer wipe(privateKey)
	return archiver.ArchiveCommit(ctx, record.SSHURL, record.SSHHost, privateKey, record.HostKeyFingerprint, commitSHA, maxBytes)
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func (s *Service) findForTenant(ctx context.Context, tenantID int, value string) (*ent.Repository, string, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil, "", ErrNotFound
	}
	record, err := s.client.Repository.Query().
		Where(entrepository.PublicIDEQ(publicID), entrepository.HasProjectWith(entproject.TenantIDEQ(tenantID))).
		WithProject().
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return record, record.Edges.Project.PublicID.String(), nil
}

func (s *Service) decryptKey(record *ent.Repository) ([]byte, error) {
	return s.box.Decrypt(record.DeployPrivateKeyCiphertext, deployKeyAADPrefix+record.PublicID.String())
}

func generateDeployKey(comment string) (string, []byte, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, err
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return "", nil, err
	}
	block, err := ssh.MarshalPrivateKey(private, comment)
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic))) + " " + comment, pem.EncodeToMemory(block), nil
}

func makeView(record *ent.Repository, projectID string, includePublicKey bool) View {
	view := View{
		ID: record.PublicID.String(), ProjectID: projectID, Name: record.Name, SSHURL: record.SSHURL,
		DefaultBranch: record.DefaultBranch, Status: string(record.Status), LastVerifiedAt: record.LastVerifiedAt,
	}
	if includePublicKey {
		view.DeployPublicKey = record.DeployPublicKey
	}
	if record.HostKeyFingerprint != "" {
		value := record.HostKeyFingerprint
		view.HostKeyFingerprint = &value
	}
	return view
}
