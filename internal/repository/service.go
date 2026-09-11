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
	"github.com/XR-Lee/Gemcp/internal/validation"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

var (
	ErrNotFound           = errors.New("repository not found")
	ErrNotActive          = errors.New("repository is not active")
	ErrVerificationFailed = errors.New("repository verification failed")
	ErrConflict           = errors.New("repository name is already registered with different settings")
	branchPattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
)

const (
	AccessPublicHTTPS = "public_https"
	AccessSSHDeploy   = "ssh_deploy_key"
)

type GitPublicProber interface {
	ProbePublicHTTPS(context.Context, string) error
}

// Official GitHub SSH host-key fingerprints from
// https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints
const (
	githubHost               = "github.com"
	githubEd25519Fingerprint = "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU"
)

const deployKeyAADPrefix = "gemcp:repository-deploy-key:v1:"

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

func invalid(message string) error { return &ValidationError{Message: message} }

type Input struct {
	ProjectID     string `json:"project_id"`
	Name          string `json:"name"`
	SSHURL        string `json:"ssh_url"`
	URL           string `json:"url"`
	DefaultBranch string `json:"default_branch"`
}

type AgentInput struct {
	Name          string `json:"name,omitempty" jsonschema:"optional display name; defaults to the GitHub repository name"`
	SSHURL        string `json:"ssh_url,omitempty" jsonschema:"GitHub SSH or HTTPS URL; alias of url"`
	URL           string `json:"url,omitempty" jsonschema:"GitHub SSH or HTTPS URL in git@github.com:owner/repository.git or https://github.com/owner/repository form"`
	DefaultBranch string `json:"default_branch,omitempty" jsonschema:"default branch; defaults to main"`
}

type AgentVerifyInput struct {
	RepositoryID    string `json:"repository_id" jsonschema:"pending repository ID returned by register_repository"`
	HostFingerprint string `json:"host_key_fingerprint,omitempty" jsonschema:"optional SHA256 SSH host fingerprint; omit to use GitHub's official pin or this Project's established pin"`
}

type ListResult struct {
	Repositories []View `json:"repositories"`
}

type View struct {
	ID                   string     `json:"id"`
	ProjectID            string     `json:"project_id"`
	Name                 string     `json:"name"`
	SSHURL               string     `json:"ssh_url"`
	DefaultBranch        string     `json:"default_branch"`
	Status               string     `json:"status"`
	DeployPublicKey      string     `json:"deploy_public_key,omitempty"`
	HostKeyFingerprint   *string    `json:"host_key_fingerprint,omitempty"`
	LastVerifiedAt       *time.Time `json:"last_verified_at,omitempty"`
	Access               string     `json:"access,omitempty"`
	DeployKeySettingsURL string     `json:"deploy_key_settings_url,omitempty"`
}

type GitVerifier interface {
	VerifyAccess(context.Context, string, string, []byte, string) error
	VerifyCommit(context.Context, string, string, []byte, string, string) error
}

type GitArchiver interface {
	ArchiveCommit(context.Context, string, string, []byte, string, string, int64) (Archive, error)
}

type GitRefResolver interface {
	ResolveRef(context.Context, string, string, []byte, string, string) (string, error)
}

type GitHEADDetector interface {
	DetectDefaultBranch(context.Context, string, string, []byte, string) (string, string, error)
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
	return s.create(ctx, s.client, tenantID, input)
}

func (s *Service) CreateForAgent(ctx context.Context, tenantID int, actorID, projectPublicID string, input AgentInput) (View, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	projectID, name, sshURL, branch, err := normalizeInput(Input{
		ProjectID: projectPublicID, Name: input.Name, SSHURL: input.SSHURL, URL: input.URL, DefaultBranch: input.DefaultBranch,
	})
	if err != nil {
		return View{}, err
	}
	existing, err := tx.Repository.Query().Where(
		entrepository.NameEQ(name),
		entrepository.HasProjectWith(entproject.PublicIDEQ(projectID), entproject.TenantIDEQ(tenantID), entproject.StatusEQ(entproject.StatusActive)),
	).WithProject().Only(ctx)
	if err == nil {
		if existing.SSHURL != sshURL || existing.DefaultBranch != branch {
			return View{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return View{}, err
		}
		return makeView(existing, projectPublicID, true), nil
	}
	if !ent.IsNotFound(err) {
		return View{}, err
	}
	view, err := s.create(ctx, tx.Client(), tenantID, Input{
		ProjectID: projectPublicID, Name: name, SSHURL: sshURL, DefaultBranch: branch,
	})
	if err != nil {
		return View{}, err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).
		SetActorType("agent_token").SetActorID(actorID).
		SetAction("repository.registered").SetTargetType("repository").SetTargetID(view.ID).
		SetMetadata(map[string]any{"project_id": projectPublicID, "name": view.Name, "ssh_url": view.SSHURL, "default_branch": view.DefaultBranch}).
		Save(ctx); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return view, nil
}

func (s *Service) create(ctx context.Context, client *ent.Client, tenantID int, input Input) (View, error) {
	var view View
	projectID, name, sshURL, branch, err := normalizeInput(input)
	if err != nil {
		return view, err
	}
	project, err := client.Project.Query().
		Where(entproject.PublicIDEQ(projectID), entproject.TenantIDEQ(tenantID), entproject.StatusEQ("active")).
		Only(ctx)
	if ent.IsNotFound(err) {
		return view, ErrNotFound
	}
	if err != nil {
		return view, err
	}
	publicID := uuid.New()
	httpsURL := githubHTTPSURLFromSSH(sshURL)
	if s.activatePublicHTTPS(ctx, httpsURL) {
		now := s.now().UTC()
		record, err := client.Repository.Create().
			SetPublicID(publicID).
			SetProjectID(project.ID).
			SetName(name).
			SetSSHURL(sshURL).
			SetSSHHost("github.com").
			SetDefaultBranch(branch).
			SetHostKeyFingerprint(githubEd25519Fingerprint).
			SetStatus("active").
			SetLastVerifiedAt(now).
			Save(ctx)
		if err != nil {
			return view, err
		}
		return makeView(record, project.PublicID.String(), true), nil
	}
	publicKey, privateKey, err := generateDeployKey("gemcp-" + publicID.String())
	if err != nil {
		return view, err
	}
	ciphertext, err := s.box.Encrypt(privateKey, deployKeyAADPrefix+publicID.String())
	if err != nil {
		return view, err
	}
	record, err := client.Repository.Create().
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

func (s *Service) activatePublicHTTPS(ctx context.Context, httpsURL string) bool {
	prober, ok := s.verifier.(GitPublicProber)
	if !ok || strings.TrimSpace(httpsURL) == "" {
		return false
	}
	return prober.ProbePublicHTTPS(ctx, httpsURL) == nil
}

func normalizeInput(input Input) (uuid.UUID, string, string, string, error) {
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return uuid.Nil, "", "", "", invalid("valid project_id is required")
	}
	remote, err := parseGitHubRemote(firstNonEmpty(input.SSHURL, input.URL))
	if err != nil {
		return uuid.Nil, "", "", "", err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = remote.Name
	}
	if name == "" || len(name) > 120 {
		return uuid.Nil, "", "", "", invalid("repository name is required")
	}
	branch := strings.TrimSpace(input.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	if !branchPattern.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "//") {
		return uuid.Nil, "", "", "", invalid("default branch is invalid")
	}
	return projectID, name, remote.SSHURL, branch, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *Service) VerifyForAgent(ctx context.Context, tenantID int, actorID, projectPublicID string, input AgentVerifyInput) (View, error) {
	projectID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return View{}, ErrNotFound
	}
	repositoryID, err := uuid.Parse(strings.TrimSpace(input.RepositoryID))
	if err != nil {
		return View{}, ErrNotFound
	}
	record, err := s.client.Repository.Query().Where(
		entrepository.PublicIDEQ(repositoryID),
		entrepository.HasProjectWith(entproject.PublicIDEQ(projectID), entproject.TenantIDEQ(tenantID), entproject.StatusEQ(entproject.StatusActive)),
	).WithProject().Only(ctx)
	if ent.IsNotFound(err) {
		return View{}, ErrNotFound
	}
	if err != nil {
		return View{}, err
	}
	fingerprint, err := s.resolveVerifyFingerprint(ctx, tenantID, projectID, record.SSHHost, input.HostFingerprint)
	if err != nil {
		return View{}, err
	}
	privateKey, err := s.decryptKey(record)
	if err != nil {
		return View{}, err
	}
	defer wipe(privateKey)
	if err := s.verifier.VerifyAccess(ctx, record.SSHURL, record.SSHHost, privateKey, fingerprint); err != nil {
		return View{}, fmt.Errorf("%w: %v", ErrVerificationFailed, err)
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	current, err := tx.Repository.Query().Where(
		entrepository.IDEQ(record.ID),
		entrepository.HasProjectWith(entproject.PublicIDEQ(projectID), entproject.TenantIDEQ(tenantID), entproject.StatusEQ(entproject.StatusActive)),
	).WithProject().Only(ctx)
	if err != nil {
		return View{}, ErrNotFound
	}
	now := s.now().UTC()
	current, err = current.Update().SetHostKeyFingerprint(fingerprint).SetStatus("active").SetLastVerifiedAt(now).Save(ctx)
	if err != nil {
		return View{}, err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).
		SetActorType("agent_token").SetActorID(actorID).
		SetAction("repository.verified").SetTargetType("repository").SetTargetID(current.PublicID.String()).
		SetMetadata(map[string]any{"project_id": projectPublicID, "ssh_url": current.SSHURL, "host_key_fingerprint": fingerprint}).
		Save(ctx); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return makeView(current, projectPublicID, true), nil
}

func validHostFingerprint(value string) bool {
	return strings.HasPrefix(value, "SHA256:") && len(value) >= 20 && len(value) <= 100
}

func officialGitHubFingerprint(host string) string {
	if host == githubHost {
		return githubEd25519Fingerprint
	}
	return ""
}

func resolveHostFingerprint(host, provided string) (string, error) {
	provided = strings.TrimSpace(provided)
	if provided != "" {
		if !validHostFingerprint(provided) {
			return "", invalid("valid SHA256 host key fingerprint is required")
		}
		return provided, nil
	}
	if fingerprint := officialGitHubFingerprint(host); fingerprint != "" {
		return fingerprint, nil
	}
	return "", invalid("valid SHA256 host key fingerprint is required")
}

func (s *Service) resolveVerifyFingerprint(ctx context.Context, tenantID int, projectID uuid.UUID, host, provided string) (string, error) {
	provided = strings.TrimSpace(provided)
	if provided != "" {
		return resolveHostFingerprint(host, provided)
	}
	if fingerprint, err := s.establishedHostFingerprint(ctx, tenantID, projectID, host); err == nil {
		return fingerprint, nil
	}
	return resolveHostFingerprint(host, "")
}

func (s *Service) establishedHostFingerprint(ctx context.Context, tenantID int, projectID uuid.UUID, host string) (string, error) {
	records, err := s.client.Repository.Query().Where(
		entrepository.SSHHostEQ(host), entrepository.StatusEQ(entrepository.StatusActive), entrepository.HostKeyFingerprintNEQ(""),
		entrepository.HasProjectWith(entproject.PublicIDEQ(projectID), entproject.TenantIDEQ(tenantID)),
	).All(ctx)
	if err != nil {
		return "", err
	}
	fingerprints := map[string]bool{}
	for _, record := range records {
		fingerprints[record.HostKeyFingerprint] = true
	}
	if len(fingerprints) != 1 {
		return "", invalid("host_key_fingerprint is required because the Project has no single established SSH host pin")
	}
	for fingerprint := range fingerprints {
		return fingerprint, nil
	}
	return "", invalid("host_key_fingerprint is required")
}

func (s *Service) Verify(ctx context.Context, tenantID int, publicID, fingerprint string) (View, error) {
	var view View
	record, projectID, err := s.findForTenant(ctx, tenantID, publicID)
	if err != nil {
		return view, err
	}
	fingerprint, err = resolveHostFingerprint(record.SSHHost, fingerprint)
	if err != nil {
		return view, err
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

func (s *Service) ResolveRef(ctx context.Context, repositoryID int, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if !branchPattern.MatchString(ref) || strings.Contains(ref, "..") || strings.Contains(ref, "//") {
		return "", invalid("Git ref is invalid")
	}
	record, err := s.client.Repository.Get(ctx, repositoryID)
	if ent.IsNotFound(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if record.Status != "active" || record.HostKeyFingerprint == "" {
		return "", ErrNotActive
	}
	resolver, ok := s.verifier.(GitRefResolver)
	if !ok {
		return "", fmt.Errorf("repository ref resolver is unavailable")
	}
	privateKey, err := s.decryptKey(record)
	if err != nil {
		return "", err
	}
	defer wipe(privateKey)
	return resolver.ResolveRef(ctx, record.SSHURL, record.SSHHost, privateKey, record.HostKeyFingerprint, ref)
}

func (s *Service) DetectDefaultBranch(ctx context.Context, repositoryID int) (string, string, error) {
	record, err := s.client.Repository.Get(ctx, repositoryID)
	if ent.IsNotFound(err) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	if record.Status != "active" || record.HostKeyFingerprint == "" {
		return "", "", ErrNotActive
	}
	detector, ok := s.verifier.(GitHEADDetector)
	if !ok {
		return record.DefaultBranch, "", nil
	}
	privateKey, err := s.decryptKey(record)
	if err != nil {
		return "", "", err
	}
	defer wipe(privateKey)
	branch, sha, err := detector.DetectDefaultBranch(ctx, record.SSHURL, record.SSHHost, privateKey, record.HostKeyFingerprint)
	if err != nil {
		return "", "", err
	}
	return branch, sha, nil
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
	if strings.TrimSpace(record.DeployPrivateKeyCiphertext) == "" {
		return nil, nil
	}
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

func AccessOf(record *ent.Repository) string {
	if record == nil {
		return ""
	}
	if strings.TrimSpace(record.DeployPrivateKeyCiphertext) == "" && record.Status == entrepository.StatusActive {
		return AccessPublicHTTPS
	}
	return AccessSSHDeploy
}

func makeView(record *ent.Repository, projectID string, includePublicKey bool) View {
	view := View{
		ID: record.PublicID.String(), ProjectID: projectID, Name: record.Name, SSHURL: record.SSHURL,
		DefaultBranch: record.DefaultBranch, Status: string(record.Status), LastVerifiedAt: record.LastVerifiedAt,
		Access: AccessOf(record),
	}
	if view.Access == AccessSSHDeploy {
		view.DeployKeySettingsURL = DeployKeySettingsURL(record.SSHURL)
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
