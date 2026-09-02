package imagebake

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/budgetentry"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/imagebake"
	entproject "github.com/XR-Lee/Gemcp/ent/project"
	entrepository "github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/researchnode"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/google/uuid"
)

const maxListedBakes = 50

var (
	bakeNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	commitPattern    = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	imageUUIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{3,127}$`)
	activeStatuses   = []imagebake.Status{
		imagebake.StatusRequested, imagebake.StatusConfirmed, imagebake.StatusProvisioning,
		imagebake.StatusInstalling, imagebake.StatusStopping, imagebake.StatusSaving,
	}
)

type ImageReader interface {
	QueryResources(context.Context, int, string) (provider.ResourceSnapshot, error)
}

type Service struct {
	client   *ent.Client
	provider Provider
	images   ImageReader
	now      func() time.Time
}

func NewService(client *ent.Client, bakeProvider Provider, images ImageReader) *Service {
	if bakeProvider == nil {
		bakeProvider = NewFailClosedProvider()
	}
	return &Service{client: client, provider: bakeProvider, images: images, now: time.Now}
}

func (s *Service) Request(ctx context.Context, principal agentauth.Principal, input RequestInput) (View, error) {
	if !principal.HasScope("configure") {
		return View{}, ErrForbidden
	}
	return s.request(ctx, principal.TenantID, principal.ProjectID, principal.ProjectPublicID, principal.TokenPublicID, imagebake.RequestedByTypeAgentToken, auditevent.ActorTypeAgentToken, input)
}

func (s *Service) OwnerRequest(ctx context.Context, tenantID int, actorID, projectPublicID string, input RequestInput) (View, error) {
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	return s.request(ctx, tenantID, projectRecord.ID, projectRecord.PublicID.String(), actorID, imagebake.RequestedByTypeUser, auditevent.ActorTypeUser, input)
}

func (s *Service) List(ctx context.Context, principal agentauth.Principal) (ListResult, error) {
	if !principal.HasScope("read") {
		return ListResult{}, ErrForbidden
	}
	return s.list(ctx, principal.TenantID, principal.ProjectID, principal.ProjectPublicID)
}

func (s *Service) OwnerList(ctx context.Context, tenantID int, projectPublicID string) (ListResult, error) {
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return ListResult{}, err
	}
	return s.list(ctx, tenantID, projectRecord.ID, projectRecord.PublicID.String())
}

func (s *Service) Get(ctx context.Context, principal agentauth.Principal, bakeID string) (View, error) {
	if !principal.HasScope("read") {
		return View{}, ErrForbidden
	}
	return s.get(ctx, principal.TenantID, principal.ProjectID, principal.ProjectPublicID, bakeID)
}

func (s *Service) OwnerGet(ctx context.Context, tenantID int, projectPublicID, bakeID string) (View, error) {
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	return s.get(ctx, tenantID, projectRecord.ID, projectRecord.PublicID.String(), bakeID)
}

func (s *Service) OwnerConfirm(ctx context.Context, tenantID int, actorID, projectPublicID, bakeID, digest string) (View, error) {
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	record, err := s.bake(ctx, projectRecord.ID, bakeID)
	if err != nil {
		return View{}, err
	}
	if record.Status != imagebake.StatusRequested {
		return View{}, ErrNotRequested
	}
	if !hmac.Equal([]byte(record.ConfirmationDigest), []byte(strings.TrimSpace(digest))) {
		return View{}, ErrDigestMismatch
	}
	now := s.now().UTC()
	record, err = record.Update().
		SetStatus(imagebake.StatusConfirmed).
		SetConfirmedBy(actorID).
		SetConfirmedAt(now).
		Save(ctx)
	if err != nil {
		return View{}, err
	}
	if _, err := s.client.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType(auditevent.ActorTypeUser).SetActorID(actorID).
		SetAction("image_bake.confirmed").SetTargetType("image_bake").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"project_id": projectPublicID, "name": record.Name}).
		Save(ctx); err != nil {
		return View{}, err
	}
	return s.runBackend(ctx, tenantID, actorID, projectPublicID, record)
}

func (s *Service) OwnerCancel(ctx context.Context, tenantID int, actorID, projectPublicID, bakeID string) (View, error) {
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	record, err := s.bake(ctx, projectRecord.ID, bakeID)
	if err != nil {
		return View{}, err
	}
	switch record.Status {
	case imagebake.StatusFinished, imagebake.StatusFailed, imagebake.StatusCancelled:
		return View{}, ErrNotCancellable
	}
	if record.InstanceUUID != "" && s.provider != nil {
		_ = s.provider.StopInstance(ctx, record.InstanceUUID)
	}
	record, err = record.Update().SetStatus(imagebake.StatusCancelled).SetFailureReason("cancelled by Owner").Save(ctx)
	if err != nil {
		return View{}, err
	}
	if _, err := s.client.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType(auditevent.ActorTypeUser).SetActorID(actorID).
		SetAction("image_bake.cancelled").SetTargetType("image_bake").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"project_id": projectPublicID}).
		Save(ctx); err != nil {
		return View{}, err
	}
	return MakeView(record, projectPublicID), nil
}

func (s *Service) OwnerOptions(ctx context.Context, tenantID int, projectPublicID string) (Options, error) {
	var result Options
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return result, err
	}
	repositories, err := s.client.Repository.Query().Where(
		entrepository.ProjectIDEQ(projectRecord.ID), entrepository.StatusEQ(entrepository.StatusActive),
	).Order(ent.Asc(entrepository.FieldName)).All(ctx)
	if err != nil {
		return result, err
	}
	result.ProjectID = projectRecord.PublicID.String()
	result.Backend = BackendAutoDLPro
	result.RecipePath = DefaultRecipe
	result.GeneratedAt = s.now().UTC()
	result.Repositories = make([]RepositoryOption, 0, len(repositories))
	for _, record := range repositories {
		result.Repositories = append(result.Repositories, RepositoryOption{
			ID: record.PublicID.String(), Name: record.Name, DefaultBranch: record.DefaultBranch,
		})
	}
	if s.images != nil {
		snapshot, queryErr := s.images.QueryResources(ctx, tenantID, "elastic")
		if queryErr == nil {
			for _, image := range append(append([]provider.Image{}, snapshot.PrivateImages...), snapshot.SystemImages...) {
				result.BaseImages = append(result.BaseImages, ImageOption{UUID: image.UUID, Name: image.Name})
			}
		}
	}
	if result.BaseImages == nil {
		result.BaseImages = []ImageOption{}
	}
	return result, nil
}

func (s *Service) request(ctx context.Context, tenantID, projectID int, projectPublicID, actorID string, actorType imagebake.RequestedByType, auditType auditevent.ActorType, input RequestInput) (View, error) {
	name := strings.TrimSpace(input.Name)
	if !bakeNamePattern.MatchString(name) {
		return View{}, invalid("name must use 1 to 100 letters, numbers, dots, underscores, or hyphens")
	}
	backend, err := normalizeBackend(input.Backend)
	if err != nil {
		return View{}, err
	}
	baseImage, err := normalizeImageUUID(input.BaseImageUUID)
	if err != nil {
		return View{}, err
	}
	commit, err := normalizeCommit(input.CommitSHA)
	if err != nil {
		return View{}, err
	}
	recipe, err := normalizeRecipe(input.RecipePath)
	if err != nil {
		return View{}, err
	}
	repositoryRecord, err := s.resolveRepository(ctx, projectID, input.RepositoryID)
	if err != nil {
		return View{}, err
	}
	busy, err := s.client.ImageBake.Query().Where(
		imagebake.ProjectIDEQ(projectID), imagebake.StatusIn(activeStatuses...),
	).Exist(ctx)
	if err != nil {
		return View{}, err
	}
	if busy {
		return View{}, ErrBusy
	}
	proposal := Proposal{
		Backend: backend, Name: name, BaseImageUUID: baseImage,
		RepositoryID: repositoryRecord.PublicID.String(), CommitSHA: commit, RecipePath: recipe,
	}
	digest := confirmationDigest(proposal, repositoryRecord.SSHURL)
	proposalJSON, _ := json.Marshal(proposal)
	var proposalMap map[string]any
	_ = json.Unmarshal(proposalJSON, &proposalMap)
	record, err := s.client.ImageBake.Create().
		SetTenantID(tenantID).
		SetProjectID(projectID).
		SetRepositoryID(repositoryRecord.ID).
		SetBackend(imagebake.Backend(backend)).
		SetName(name).
		SetBaseImageUUID(baseImage).
		SetCommitSha(commit).
		SetRecipePath(recipe).
		SetStatus(imagebake.StatusRequested).
		SetConfirmationDigest(digest).
		SetRequestedBy(actorID).
		SetRequestedByType(actorType).
		SetProposal(proposalMap).
		SetEstimatedCostMilli(0).
		Save(ctx)
	if err != nil {
		return View{}, err
	}
	if _, err := s.client.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType(auditType).SetActorID(actorID).
		SetAction("image_bake.requested").SetTargetType("image_bake").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": projectPublicID, "name": name, "backend": backend, "base_image_uuid": baseImage,
		}).Save(ctx); err != nil {
		return View{}, err
	}
	return MakeView(record, projectPublicID), nil
}

func (s *Service) runBackend(ctx context.Context, tenantID int, actorID, projectPublicID string, record *ent.ImageBake) (View, error) {
	fail := func(reason string) (View, error) {
		updated, err := record.Update().SetStatus(imagebake.StatusFailed).SetFailureReason(bounded(reason, 512)).Save(ctx)
		if err != nil {
			return View{}, err
		}
		_, _ = s.client.AuditEvent.Create().
			SetTenantID(tenantID).SetActorType(auditevent.ActorTypeUser).SetActorID(actorID).
			SetAction("image_bake.failed").SetTargetType("image_bake").SetTargetID(updated.PublicID.String()).
			SetMetadata(map[string]any{"project_id": projectPublicID, "reason": bounded(reason, 240)}).
			Save(ctx)
		return MakeView(updated, projectPublicID), nil
	}
	spec := InstanceSpec{
		Name: record.Name, BaseImageUUID: record.BaseImageUUID,
		RecipePath: record.RecipePath, CommitSHA: record.CommitSha,
	}
	instanceUUID, err := s.provider.CreateInstance(ctx, spec)
	if err != nil {
		return fail(err.Error())
	}
	record, err = record.Update().SetStatus(imagebake.StatusProvisioning).SetInstanceUUID(instanceUUID).Save(ctx)
	if err != nil {
		return View{}, err
	}
	if err := s.provider.RunRecipe(ctx, instanceUUID, record.RecipePath, record.CommitSha); err != nil {
		return fail(err.Error())
	}
	record, err = record.Update().SetStatus(imagebake.StatusInstalling).Save(ctx)
	if err != nil {
		return View{}, err
	}
	if err := s.provider.StopInstance(ctx, instanceUUID); err != nil {
		return fail(err.Error())
	}
	record, err = record.Update().SetStatus(imagebake.StatusStopping).Save(ctx)
	if err != nil {
		return View{}, err
	}
	imageUUID, err := s.provider.SaveImage(ctx, instanceUUID, record.Name)
	if err != nil {
		return fail(err.Error())
	}
	record, err = record.Update().SetStatus(imagebake.StatusSaving).Save(ctx)
	if err != nil {
		return View{}, err
	}
	record, err = record.Update().SetStatus(imagebake.StatusFinished).SetImageUUID(imageUUID).Save(ctx)
	if err != nil {
		return View{}, err
	}
	if _, err := s.client.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType(auditevent.ActorTypeUser).SetActorID(actorID).
		SetAction("image_bake.finished").SetTargetType("image_bake").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"project_id": projectPublicID, "image_uuid": imageUUID}).
		Save(ctx); err != nil {
		return View{}, err
	}
	return MakeView(record, projectPublicID), nil
}

func (s *Service) list(ctx context.Context, tenantID, projectID int, projectPublicID string) (ListResult, error) {
	_ = tenantID
	records, err := s.client.ImageBake.Query().Where(imagebake.ProjectIDEQ(projectID)).
		Order(ent.Desc(imagebake.FieldCreatedAt)).Limit(maxListedBakes).All(ctx)
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Bakes: make([]View, 0, len(records))}
	for _, record := range records {
		result.Bakes = append(result.Bakes, MakeView(record, projectPublicID))
	}
	return result, nil
}

func (s *Service) get(ctx context.Context, tenantID, projectID int, projectPublicID, bakeID string) (View, error) {
	_ = tenantID
	record, err := s.bake(ctx, projectID, bakeID)
	if err != nil {
		return View{}, err
	}
	return MakeView(record, projectPublicID), nil
}

func (s *Service) bake(ctx context.Context, projectID int, bakeID string) (*ent.ImageBake, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(bakeID))
	if err != nil {
		return nil, ErrNotFound
	}
	record, err := s.client.ImageBake.Query().Where(
		imagebake.PublicIDEQ(publicID), imagebake.ProjectIDEQ(projectID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	return record, err
}

func (s *Service) activeProject(ctx context.Context, tenantID int, projectPublicID string) (*ent.Project, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return nil, ErrProject
	}
	record, err := s.client.Project.Query().Where(
		entproject.PublicIDEQ(publicID), entproject.TenantIDEQ(tenantID), entproject.StatusEQ(entproject.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrProject
	}
	return record, err
}

func (s *Service) resolveRepository(ctx context.Context, projectID int, repositoryID string) (*ent.Repository, error) {
	query := s.client.Repository.Query().Where(
		entrepository.ProjectIDEQ(projectID), entrepository.StatusEQ(entrepository.StatusActive),
	)
	if strings.TrimSpace(repositoryID) != "" {
		publicID, err := uuid.Parse(strings.TrimSpace(repositoryID))
		if err != nil {
			return nil, invalid("repository_id must be an active repository")
		}
		record, err := query.Where(entrepository.PublicIDEQ(publicID)).Only(ctx)
		if ent.IsNotFound(err) {
			return nil, invalid("repository must be active and belong to the Project")
		}
		return record, err
	}
	records, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	if len(records) == 1 {
		return records[0], nil
	}
	if len(records) == 0 {
		return nil, invalid("an active repository is required")
	}
	return nil, invalid("repository_id is required when the Project has more than one repository")
}

func (s *Service) SideEffectCounts(ctx context.Context, projectID int) (experiments, nodes, budget int, err error) {
	if experiments, err = s.client.Experiment.Query().Where(entexperiment.ProjectIDEQ(projectID)).Count(ctx); err != nil {
		return
	}
	if nodes, err = s.client.ResearchNode.Query().Where(researchnode.ProjectIDEQ(projectID)).Count(ctx); err != nil {
		return
	}
	budget, err = s.client.BudgetEntry.Query().Where(budgetentry.ProjectIDEQ(projectID)).Count(ctx)
	return
}

func MakeView(record *ent.ImageBake, projectID string) View {
	proposal := Proposal{
		Backend: string(record.Backend), Name: record.Name, BaseImageUUID: record.BaseImageUUID,
		CommitSHA: record.CommitSha, RecipePath: record.RecipePath,
	}
	if encoded, err := json.Marshal(record.Proposal); err == nil {
		_ = json.Unmarshal(encoded, &proposal)
	}
	view := View{
		ID: record.PublicID.String(), ProjectID: projectID, RepositoryID: proposal.RepositoryID, Name: record.Name,
		Backend: string(record.Backend), BaseImageUUID: record.BaseImageUUID, CommitSHA: record.CommitSha,
		RecipePath: record.RecipePath, Status: string(record.Status), ConfirmationDigest: record.ConfirmationDigest,
		RequestedBy: record.RequestedBy, RequestedByType: string(record.RequestedByType),
		ConfirmedBy: record.ConfirmedBy, ConfirmedAt: record.ConfirmedAt, ImageUUID: record.ImageUUID,
		InstanceUUID: record.InstanceUUID, FailureReason: record.FailureReason, Proposal: proposal,
		EstimatedCostMilli: record.EstimatedCostMilli, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
	return view
}

func confirmationDigest(proposal Proposal, repositorySSHURL string) string {
	material := struct {
		Proposal           Proposal `json:"proposal"`
		RepositorySSHURL   string   `json:"repository_ssh_url"`
		EstimatedCostMilli int64    `json:"estimated_cost_milli"`
	}{Proposal: proposal, RepositorySSHURL: repositorySSHURL, EstimatedCostMilli: 0}
	encoded, _ := json.Marshal(material)
	hash := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", hash[:])
}

func normalizeBackend(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", BackendAutoDLPro, "pro", "autodl-pro":
		return BackendAutoDLPro, nil
	default:
		return "", invalid("backend must be autodl_pro")
	}
}

func normalizeImageUUID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !imageUUIDPattern.MatchString(value) || !utf8.ValidString(value) {
		return "", invalid("base_image_uuid must be a Provider image identifier")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", invalid("base_image_uuid must not contain control characters")
		}
	}
	return value, nil
}

func normalizeCommit(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !commitPattern.MatchString(value) {
		return "", invalid("commit_sha must be a 40 or 64 character hexadecimal SHA")
	}
	return value, nil
}

func normalizeRecipe(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultRecipe, nil
	}
	if strings.Contains(value, "\\") || strings.HasPrefix(value, "/") || strings.Contains(value, ":") {
		return "", invalid("recipe_path must be a relative POSIX path")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", invalid("recipe_path must stay inside the repository")
	}
	return cleaned, nil
}

func bounded(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}
