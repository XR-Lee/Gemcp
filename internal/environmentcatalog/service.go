package environmentcatalog

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/imagebake"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/provider"
	"github.com/XR-Lee/Gemcp/internal/validation"
	"github.com/google/uuid"
)

const (
	BackendElastic  = "autodl_elastic"
	BackendPrivate  = "autodl_private"
	maxEnvironments = 16
)

var (
	ErrNotFound  = errors.New("environment not found")
	ErrProject   = errors.New("active Project was not found")
	ErrConflict  = errors.New("environment name is already registered")
	ErrLimit     = errors.New("environment limit reached")
	ErrForbidden = errors.New("forbidden")
	ErrImage     = errors.New("image UUID is not visible to this Project")
)

var (
	environmentNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	imageUUIDPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{3,127}$`)
	officialImagePattern   = regexp.MustCompile(`^image-[0-9a-fA-F]{8,}$`)
)

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

func invalid(message string) error { return &ValidationError{Message: message} }

type ImageReader interface {
	QueryResources(context.Context, int, string) (provider.ResourceSnapshot, error)
}

type RegisterInput struct {
	Name       string `json:"name" jsonschema:"stable Environment name using letters, numbers, dot, underscore, or hyphen"`
	Backend    string `json:"backend,omitempty" jsonschema:"autodl_elastic or autodl_private; defaults to autodl_elastic"`
	ImageUUID  string `json:"image_uuid" jsonschema:"Provider-visible AutoDL image UUID"`
	SetDefault bool   `json:"set_default,omitempty" jsonschema:"make this the default Environment for the backend"`
}

type RemoveInput struct {
	EnvironmentID string `json:"environment_id" jsonschema:"registered Environment ID"`
}

type View struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Backend   string `json:"backend"`
	ImageUUID string `json:"image_uuid"`
	IsDefault bool   `json:"is_default"`
	Status    string `json:"status"`
}

type ListResult struct {
	Environments []View `json:"environments"`
}

type Service struct {
	client *ent.Client
	images ImageReader
}

func NewService(client *ent.Client, images ImageReader) *Service {
	return &Service{client: client, images: images}
}

func (s *Service) Register(ctx context.Context, principal agentauth.Principal, input RegisterInput) (View, error) {
	if !principal.HasScope("configure") {
		return View{}, ErrForbidden
	}
	return s.register(ctx, principal.TenantID, principal.ProjectID, principal.ProjectPublicID, principal.TokenPublicID, auditevent.ActorTypeAgentToken, input, false)
}

func (s *Service) OwnerRegister(ctx context.Context, tenantID int, actorID, projectPublicID string, input RegisterInput) (View, error) {
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	return s.register(ctx, tenantID, projectRecord.ID, projectRecord.PublicID.String(), actorID, auditevent.ActorTypeUser, input, true)
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

func (s *Service) Remove(ctx context.Context, principal agentauth.Principal, input RemoveInput) (View, error) {
	if !principal.HasScope("configure") {
		return View{}, ErrForbidden
	}
	return s.remove(ctx, principal.TenantID, principal.ProjectID, principal.ProjectPublicID, principal.TokenPublicID, auditevent.ActorTypeAgentToken, input.EnvironmentID)
}

func (s *Service) OwnerRemove(ctx context.Context, tenantID int, actorID, projectPublicID, environmentID string) (View, error) {
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	return s.remove(ctx, tenantID, projectRecord.ID, projectRecord.PublicID.String(), actorID, auditevent.ActorTypeUser, environmentID)
}

func (s *Service) register(ctx context.Context, tenantID, projectID int, projectPublicID, actorID string, actorType auditevent.ActorType, input RegisterInput, allowOfficial bool) (View, error) {
	name := strings.TrimSpace(input.Name)
	if !environmentNamePattern.MatchString(name) {
		return View{}, invalid("name must use 1 to 100 letters, numbers, dots, underscores, or hyphens")
	}
	backend, err := NormalizeBackend(input.Backend)
	if err != nil {
		return View{}, err
	}
	imageUUID, err := NormalizeImageUUID(input.ImageUUID)
	if err != nil {
		return View{}, err
	}
	if err := s.authorizeImage(ctx, tenantID, projectID, backend, imageUUID, allowOfficial); err != nil {
		return View{}, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	existing, err := tx.Environment.Query().Where(
		environment.ProjectIDEQ(projectID), environment.NameEQ(name),
	).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return View{}, err
	}
	var record *ent.Environment
	if existing != nil {
		if string(existing.Backend) != backend {
			return View{}, ErrConflict
		}
		update := existing.Update().SetImageUUID(imageUUID).SetStatus(environment.StatusApproved)
		if input.SetDefault {
			if _, err := tx.Environment.Update().Where(environment.ProjectIDEQ(projectID), environment.BackendEQ(environment.Backend(backend))).SetIsDefault(false).Save(ctx); err != nil {
				return View{}, err
			}
			update.SetIsDefault(true)
		}
		record, err = update.Save(ctx)
		if err != nil {
			return View{}, err
		}
	} else {
		activeCount, countErr := tx.Environment.Query().Where(
			environment.ProjectIDEQ(projectID), environment.StatusEQ(environment.StatusApproved),
		).Count(ctx)
		if countErr != nil {
			return View{}, countErr
		}
		if activeCount >= maxEnvironments {
			return View{}, ErrLimit
		}
		create := tx.Environment.Create().
			SetProjectID(projectID).
			SetBackend(environment.Backend(backend)).
			SetName(name).
			SetImageUUID(imageUUID).
			SetStatus(environment.StatusApproved).
			SetIsDefault(input.SetDefault)
		if input.SetDefault {
			if _, err := tx.Environment.Update().Where(environment.ProjectIDEQ(projectID), environment.BackendEQ(environment.Backend(backend))).SetIsDefault(false).Save(ctx); err != nil {
				return View{}, err
			}
		}
		record, err = create.Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return View{}, ErrConflict
			}
			return View{}, err
		}
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).
		SetActorType(actorType).
		SetActorID(actorID).
		SetAction("environment.registered").
		SetTargetType("environment").
		SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": projectPublicID, "name": name, "backend": backend, "image_uuid": imageUUID,
			"set_default": input.SetDefault,
		}).Save(ctx); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return MakeView(record, projectPublicID), nil
}

func (s *Service) list(ctx context.Context, tenantID, projectID int, projectPublicID string) (ListResult, error) {
	_ = tenantID
	records, err := s.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectID),
	).Order(ent.Desc(environment.FieldIsDefault), ent.Asc(environment.FieldBackend), ent.Asc(environment.FieldName)).All(ctx)
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Environments: make([]View, 0, len(records))}
	for _, record := range records {
		result.Environments = append(result.Environments, MakeView(record, projectPublicID))
	}
	return result, nil
}

func (s *Service) remove(ctx context.Context, tenantID, projectID int, projectPublicID, actorID string, actorType auditevent.ActorType, environmentID string) (View, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(environmentID))
	if err != nil {
		return View{}, ErrNotFound
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	record, err := tx.Environment.Query().Where(
		environment.PublicIDEQ(publicID), environment.ProjectIDEQ(projectID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return View{}, ErrNotFound
	}
	if err != nil {
		return View{}, err
	}
	if record.Status != environment.StatusDisabled {
		wasDefault := record.IsDefault
		update := record.Update().SetStatus(environment.StatusDisabled)
		if wasDefault {
			update.SetIsDefault(false)
		}
		record, err = update.Save(ctx)
		if err != nil {
			return View{}, err
		}
		if wasDefault {
			replacement, replaceErr := tx.Environment.Query().Where(
				environment.ProjectIDEQ(projectID), environment.BackendEQ(record.Backend),
				environment.StatusEQ(environment.StatusApproved), environment.IDNEQ(record.ID),
			).Order(ent.Asc(environment.FieldID)).First(ctx)
			if replaceErr == nil {
				if _, err := replacement.Update().SetIsDefault(true).Save(ctx); err != nil {
					return View{}, err
				}
			}
		}
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType(actorType).SetActorID(actorID).
		SetAction("environment.disabled").SetTargetType("environment").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"project_id": projectPublicID, "name": record.Name, "backend": string(record.Backend)}).
		Save(ctx); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return MakeView(record, projectPublicID), nil
}

func (s *Service) authorizeImage(ctx context.Context, tenantID, projectID int, backend, imageUUID string, allowOfficial bool) error {
	existing, err := s.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectID), environment.ImageUUIDEQ(imageUUID),
	).Exist(ctx)
	if err != nil {
		return err
	}
	if existing {
		return nil
	}
	baked, bakeErr := s.client.ImageBake.Query().Where(
		imagebake.ProjectIDEQ(projectID), imagebake.StatusEQ(imagebake.StatusFinished), imagebake.ImageUUIDEQ(imageUUID),
	).Exist(ctx)
	if bakeErr != nil {
		return bakeErr
	}
	if baked {
		return nil
	}
	providerBackend := "elastic"
	if backend == BackendPrivate {
		providerBackend = "private"
	}
	if s.images != nil {
		snapshot, queryErr := s.images.QueryResources(ctx, tenantID, providerBackend)
		if queryErr == nil && imageVisible(snapshot, imageUUID) {
			return nil
		}
	}
	if allowOfficial && officialImagePattern.MatchString(imageUUID) {
		return nil
	}
	if s.images == nil && allowOfficial {
		return nil
	}
	return ErrImage
}

func (s *Service) activeProject(ctx context.Context, tenantID int, projectPublicID string) (*ent.Project, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return nil, ErrProject
	}
	record, err := s.client.Project.Query().Where(
		project.PublicIDEQ(publicID), project.TenantIDEQ(tenantID), project.StatusEQ(project.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrProject
	}
	return record, err
}

func imageVisible(snapshot provider.ResourceSnapshot, imageUUID string) bool {
	for _, image := range append(append([]provider.Image{}, snapshot.PrivateImages...), snapshot.SystemImages...) {
		if image.UUID == imageUUID {
			return true
		}
	}
	return false
}

func NormalizeBackend(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", BackendElastic, "elastic", "public-elastic":
		return BackendElastic, nil
	case BackendPrivate, "private":
		return BackendPrivate, nil
	default:
		return "", invalid("backend must be autodl_elastic or autodl_private")
	}
}

func NormalizeImageUUID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !imageUUIDPattern.MatchString(value) || !utf8.ValidString(value) {
		return "", invalid("image_uuid must be a Provider image identifier")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", invalid("image_uuid must not contain control characters")
		}
	}
	return value, nil
}

func MakeView(record *ent.Environment, projectID string) View {
	return View{
		ID: record.PublicID.String(), ProjectID: projectID, Name: record.Name,
		Backend: string(record.Backend), ImageUUID: record.ImageUUID, IsDefault: record.IsDefault, Status: string(record.Status),
	}
}
