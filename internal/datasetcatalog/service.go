package datasetcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/datasetbinding"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/validation"
	"github.com/XR-Lee/Gemcp/internal/workspacecatalog"
	"github.com/google/uuid"
)

const (
	BackendElastic = "autodl_elastic"
	BackendPrivate = "autodl_private"
	autoDLRoot     = "/root/autodl-fs/"
	maxBindings    = 32
	maxMarkers     = 16
)

var (
	ErrNotFound  = errors.New("dataset binding not found")
	ErrProject   = errors.New("active Project was not found")
	ErrConflict  = errors.New("dataset binding name or environment variable is already registered")
	ErrLimit     = errors.New("dataset binding limit reached")
	ErrForbidden = errors.New("forbidden")
)

type validationDomain struct{}

type ValidationError = validation.Error[validationDomain]

func invalid(message string) error { return &ValidationError{Message: message} }

type RegisterInput struct {
	Name            string   `json:"name" jsonschema:"stable dataset name using letters, numbers, dot, underscore, or hyphen"`
	Backend         string   `json:"backend,omitempty" jsonschema:"autodl_elastic or autodl_private; defaults to autodl_elastic"`
	CanonicalRoot   string   `json:"canonical_root" jsonschema:"absolute AutoDL file-storage path under /root/autodl-fs/"`
	RequiredMarkers []string `json:"required_markers,omitempty" jsonschema:"optional relative files that must exist under the canonical root"`
}

type RemoveInput struct {
	BindingID string `json:"binding_id" jsonschema:"registered dataset binding ID"`
}

type View struct {
	ID                  string   `json:"id"`
	ProjectID           string   `json:"project_id"`
	Name                string   `json:"name"`
	Backend             string   `json:"backend"`
	CanonicalRoot       string   `json:"canonical_root"`
	EnvironmentVariable string   `json:"environment_variable"`
	RequiredMarkers     []string `json:"required_markers"`
	Status              string   `json:"status"`
}

type ListResult struct {
	Bindings []View `json:"dataset_bindings"`
}

type Service struct{ client *ent.Client }

func NewService(client *ent.Client) *Service { return &Service{client: client} }

func (s *Service) Register(ctx context.Context, principal agentauth.Principal, input RegisterInput) (View, error) {
	if !principal.HasScope("configure") {
		return View{}, ErrForbidden
	}
	return s.register(ctx, principal.TenantID, principal.ProjectID, principal.ProjectPublicID, &principal.TokenID, principal.TokenPublicID, auditevent.ActorTypeAgentToken, input)
}

func (s *Service) OwnerRegister(ctx context.Context, tenantID int, actorID, projectPublicID string, input RegisterInput) (View, error) {
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	return s.register(ctx, tenantID, projectRecord.ID, projectRecord.PublicID.String(), nil, actorID, auditevent.ActorTypeUser, input)
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
	return s.remove(ctx, principal.TenantID, principal.ProjectID, principal.ProjectPublicID, principal.TokenPublicID, auditevent.ActorTypeAgentToken, input.BindingID)
}

func (s *Service) OwnerRemove(ctx context.Context, tenantID int, actorID, projectPublicID, bindingID string) (View, error) {
	projectRecord, err := s.activeProject(ctx, tenantID, projectPublicID)
	if err != nil {
		return View{}, err
	}
	return s.remove(ctx, tenantID, projectRecord.ID, projectRecord.PublicID.String(), actorID, auditevent.ActorTypeUser, bindingID)
}

func (s *Service) register(ctx context.Context, tenantID, projectID int, projectPublicID string, tokenID *int, actorID string, actorType auditevent.ActorType, input RegisterInput) (View, error) {
	name := strings.TrimSpace(input.Name)
	if !workspacecatalog.ValidDatasetName(name) {
		return View{}, invalid("name must use 1 to 100 letters, numbers, dots, underscores, or hyphens")
	}
	backend, err := NormalizeBackend(input.Backend)
	if err != nil {
		return View{}, err
	}
	canonicalRoot, err := NormalizeCanonicalRoot(input.CanonicalRoot)
	if err != nil {
		return View{}, err
	}
	markers, err := NormalizeMarkers(input.RequiredMarkers)
	if err != nil {
		return View{}, err
	}
	environmentVariable := workspacecatalog.DatasetEnvironmentVariable(name)
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	existing, err := tx.DatasetBinding.Query().Where(
		datasetbinding.ProjectIDEQ(projectID), datasetbinding.BackendEQ(datasetbinding.Backend(backend)),
		datasetbinding.Or(datasetbinding.NameEQ(name), datasetbinding.EnvironmentVariableEQ(environmentVariable)),
	).All(ctx)
	if err != nil {
		return View{}, err
	}
	if len(existing) > 1 || len(existing) == 1 && (existing[0].Name != name || existing[0].EnvironmentVariable != environmentVariable) {
		return View{}, ErrConflict
	}
	var record *ent.DatasetBinding
	if len(existing) == 1 {
		record, err = existing[0].Update().
			SetCanonicalRoot(canonicalRoot).
			SetRequiredMarkers(markers).
			SetStatus(datasetbinding.StatusActive).
			Save(ctx)
		if err != nil {
			return View{}, err
		}
	} else {
		activeCount, countErr := tx.DatasetBinding.Query().Where(
			datasetbinding.ProjectIDEQ(projectID), datasetbinding.BackendEQ(datasetbinding.Backend(backend)),
			datasetbinding.StatusEQ(datasetbinding.StatusActive),
		).Count(ctx)
		if countErr != nil {
			return View{}, countErr
		}
		if activeCount >= maxBindings {
			return View{}, ErrLimit
		}
		create := tx.DatasetBinding.Create().
			SetTenantID(tenantID).
			SetProjectID(projectID).
			SetName(name).
			SetBackend(datasetbinding.Backend(backend)).
			SetCanonicalRoot(canonicalRoot).
			SetEnvironmentVariable(environmentVariable).
			SetRequiredMarkers(markers)
		if tokenID != nil {
			create.SetAgentTokenID(*tokenID)
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
		SetAction("dataset.binding_registered").
		SetTargetType("dataset_binding").
		SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": projectPublicID, "name": name, "backend": backend,
			"canonical_root": canonicalRoot, "required_markers": markers,
		}).Save(ctx); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return MakeView(record, projectPublicID), nil
}

func (s *Service) list(ctx context.Context, tenantID, projectID int, projectPublicID string) (ListResult, error) {
	records, err := s.client.DatasetBinding.Query().Where(
		datasetbinding.ProjectIDEQ(projectID), datasetbinding.TenantIDEQ(tenantID),
	).Order(ent.Asc(datasetbinding.FieldBackend), ent.Asc(datasetbinding.FieldName)).All(ctx)
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Bindings: make([]View, 0, len(records))}
	for _, record := range records {
		result.Bindings = append(result.Bindings, MakeView(record, projectPublicID))
	}
	return result, nil
}

func (s *Service) remove(ctx context.Context, tenantID, projectID int, projectPublicID, actorID string, actorType auditevent.ActorType, bindingID string) (View, error) {
	publicID, err := uuid.Parse(strings.TrimSpace(bindingID))
	if err != nil {
		return View{}, ErrNotFound
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	record, err := tx.DatasetBinding.Query().Where(
		datasetbinding.PublicIDEQ(publicID), datasetbinding.ProjectIDEQ(projectID), datasetbinding.TenantIDEQ(tenantID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return View{}, ErrNotFound
	}
	if err != nil {
		return View{}, err
	}
	if record.Status != datasetbinding.StatusDisabled {
		record, err = record.Update().SetStatus(datasetbinding.StatusDisabled).Save(ctx)
		if err != nil {
			return View{}, err
		}
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType(actorType).SetActorID(actorID).
		SetAction("dataset.binding_disabled").SetTargetType("dataset_binding").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"project_id": projectPublicID, "name": record.Name, "backend": string(record.Backend)}).
		Save(ctx); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return MakeView(record, projectPublicID), nil
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

func NormalizeCanonicalRoot(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || strings.Contains(value, "\\") || !path.IsAbs(value) {
		return "", invalid("canonical_root must be an absolute UTF-8 path under /root/autodl-fs/")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", invalid("canonical_root must not contain control characters")
		}
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "/root/autodl-fs" || !strings.HasPrefix(cleaned, autoDLRoot) {
		return "", invalid("canonical_root must be a normalized path below /root/autodl-fs/")
	}
	return cleaned, nil
}

func NormalizeMarkers(values []string) ([]string, error) {
	if len(values) > maxMarkers {
		return nil, invalid("required_markers is limited to 16 relative files")
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		normalized, err := workspacecatalog.NormalizeRelativePath(value)
		if err != nil {
			return nil, invalid("required_markers must be normalized relative files under the canonical root")
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result, nil
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

func MakeView(record *ent.DatasetBinding, projectID string) View {
	markers := append([]string(nil), record.RequiredMarkers...)
	if markers == nil {
		markers = []string{}
	}
	return View{
		ID: record.PublicID.String(), ProjectID: projectID, Name: record.Name, Backend: string(record.Backend),
		CanonicalRoot: record.CanonicalRoot, EnvironmentVariable: record.EnvironmentVariable,
		RequiredMarkers: markers, Status: string(record.Status),
	}
}

func ViewsFromRecords(records []*ent.DatasetBinding, projectID string) []View {
	result := make([]View, 0, len(records))
	for _, record := range records {
		result = append(result, MakeView(record, projectID))
	}
	return result
}

func Snapshot(views []View) []map[string]any {
	result := make([]map[string]any, 0, len(views))
	for _, view := range views {
		markers := append([]string(nil), view.RequiredMarkers...)
		if markers == nil {
			markers = []string{}
		}
		result = append(result, map[string]any{
			"id": view.ID, "name": view.Name, "backend": view.Backend,
			"canonical_root": view.CanonicalRoot, "environment_variable": view.EnvironmentVariable,
			"required_markers": markers,
		})
	}
	return result
}

func EqualSnapshots(left, right []map[string]any) bool {
	encodedLeft, leftErr := json.Marshal(left)
	encodedRight, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(encodedLeft) == string(encodedRight)
}
