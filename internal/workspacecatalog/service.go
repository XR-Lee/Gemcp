package workspacecatalog

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/ent/workspacedataset"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/google/uuid"
)

var (
	ErrNotFound             = errors.New("workspace dataset not found")
	ErrTrustedWorkspace     = errors.New("trusted workspace authorization was not found")
	ErrWorkspaceChoice      = errors.New("workspace selector is required")
	ErrDatasetConflict      = errors.New("workspace dataset name or path is already registered")
	ErrDatasetLimit         = errors.New("workspace dataset limit reached")
	ErrForbidden            = errors.New("forbidden")
	datasetNamePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	nonEnvironmentCharacter = regexp.MustCompile(`[^A-Z0-9]+`)
)

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

type RegisterInput struct {
	Name         string `json:"name" jsonschema:"stable dataset name using letters, numbers, dot, underscore, or hyphen"`
	RelativePath string `json:"relative_path" jsonschema:"normalized path relative to the Owner-approved trusted workspace root"`
	Workspace    string `json:"workspace,omitempty" jsonschema:"trusted workspace Node label or ID; omit when the Project has exactly one"`
}

type RemoveInput struct {
	DatasetID string `json:"dataset_id" jsonschema:"registered workspace dataset ID"`
}

type View struct {
	ID                  string `json:"id"`
	ProjectID           string `json:"project_id"`
	NodeID              string `json:"node_id"`
	NodeLabel           string `json:"node_label"`
	Name                string `json:"name"`
	RelativePath        string `json:"relative_path"`
	ContainerPath       string `json:"container_path"`
	EnvironmentVariable string `json:"environment_variable"`
	Status              string `json:"status"`
}

type ListResult struct {
	Datasets []View `json:"datasets"`
}

type Service struct{ client *ent.Client }

func NewService(client *ent.Client) *Service { return &Service{client: client} }

func (s *Service) Register(ctx context.Context, principal agentauth.Principal, input RegisterInput) (View, error) {
	if !principal.HasScope("configure") {
		return View{}, ErrForbidden
	}
	name := strings.TrimSpace(input.Name)
	if !datasetNamePattern.MatchString(name) {
		return View{}, invalid("name must use 1 to 100 letters, numbers, dots, underscores, or hyphens")
	}
	relativePath, err := NormalizeRelativePath(input.RelativePath)
	if err != nil {
		return View{}, err
	}
	projectRecord, access, node, err := s.resolveWorkspace(ctx, principal, input.Workspace)
	if err != nil {
		return View{}, err
	}
	environmentVariable := DatasetEnvironmentVariable(name)

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	existing, err := tx.WorkspaceDataset.Query().Where(
		workspacedataset.ProjectIDEQ(projectRecord.ID), workspacedataset.NodeIDEQ(node.ID),
		workspacedataset.Or(
			workspacedataset.NameEQ(name), workspacedataset.RelativePathEQ(relativePath),
			workspacedataset.EnvironmentVariableEQ(environmentVariable),
		),
	).WithProject().WithNode().All(ctx)
	if err != nil {
		return View{}, err
	}
	if len(existing) > 1 || len(existing) == 1 && (existing[0].Name != name || existing[0].RelativePath != relativePath) {
		return View{}, ErrDatasetConflict
	}
	var record *ent.WorkspaceDataset
	if len(existing) == 1 {
		record = existing[0]
		if record.Status != workspacedataset.StatusActive {
			record, err = record.Update().SetStatus(workspacedataset.StatusActive).Save(ctx)
			if err != nil {
				return View{}, err
			}
		}
	} else {
		activeCount, countErr := tx.WorkspaceDataset.Query().Where(
			workspacedataset.ProjectIDEQ(projectRecord.ID), workspacedataset.NodeIDEQ(node.ID), workspacedataset.StatusEQ(workspacedataset.StatusActive),
		).Count(ctx)
		if countErr != nil {
			return View{}, countErr
		}
		if activeCount >= 32 {
			return View{}, ErrDatasetLimit
		}
		record, err = tx.WorkspaceDataset.Create().
			SetTenantID(principal.TenantID).
			SetProjectID(projectRecord.ID).
			SetNodeID(node.ID).
			SetAgentTokenID(principal.TokenID).
			SetName(name).
			SetRelativePath(relativePath).
			SetEnvironmentVariable(environmentVariable).
			Save(ctx)
		if err != nil {
			if ent.IsConstraintError(err) {
				return View{}, ErrDatasetConflict
			}
			return View{}, err
		}
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(principal.TenantID).
		SetActorType("agent_token").
		SetActorID(principal.TokenPublicID).
		SetAction("workspace.dataset_registered").
		SetTargetType("workspace_dataset").
		SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": projectRecord.PublicID.String(), "node_id": node.PublicID.String(),
			"name": name, "relative_path": relativePath, "workspace_path": *access.WorkspacePath,
		}).Save(ctx); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return makeView(record, projectRecord.PublicID.String(), node), nil
}

func (s *Service) List(ctx context.Context, principal agentauth.Principal) (ListResult, error) {
	if !principal.HasScope("read") {
		return ListResult{}, ErrForbidden
	}
	records, err := s.client.WorkspaceDataset.Query().Where(
		workspacedataset.ProjectIDEQ(principal.ProjectID), workspacedataset.TenantIDEQ(principal.TenantID),
	).WithProject().WithNode().Order(ent.Asc(workspacedataset.FieldName)).All(ctx)
	if err != nil {
		return ListResult{}, err
	}
	result := ListResult{Datasets: make([]View, 0, len(records))}
	for _, record := range records {
		node, edgeErr := record.Edges.NodeOrErr()
		if edgeErr != nil {
			return ListResult{}, edgeErr
		}
		projectRecord, edgeErr := record.Edges.ProjectOrErr()
		if edgeErr != nil {
			return ListResult{}, edgeErr
		}
		result.Datasets = append(result.Datasets, makeView(record, projectRecord.PublicID.String(), node))
	}
	return result, nil
}

func (s *Service) Remove(ctx context.Context, principal agentauth.Principal, input RemoveInput) (View, error) {
	if !principal.HasScope("configure") {
		return View{}, ErrForbidden
	}
	publicID, err := uuid.Parse(strings.TrimSpace(input.DatasetID))
	if err != nil {
		return View{}, ErrNotFound
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return View{}, err
	}
	defer tx.Rollback()
	record, err := tx.WorkspaceDataset.Query().Where(
		workspacedataset.PublicIDEQ(publicID), workspacedataset.ProjectIDEQ(principal.ProjectID), workspacedataset.TenantIDEQ(principal.TenantID),
	).WithProject().WithNode().Only(ctx)
	if ent.IsNotFound(err) {
		return View{}, ErrNotFound
	}
	if err != nil {
		return View{}, err
	}
	node, err := record.Edges.NodeOrErr()
	if err != nil {
		return View{}, err
	}
	if record.Status != workspacedataset.StatusDisabled {
		record, err = record.Update().SetStatus(workspacedataset.StatusDisabled).Save(ctx)
		if err != nil {
			return View{}, err
		}
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(principal.TenantID).
		SetActorType("agent_token").SetActorID(principal.TokenPublicID).
		SetAction("workspace.dataset_disabled").SetTargetType("workspace_dataset").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"project_id": principal.ProjectPublicID, "name": record.Name, "relative_path": record.RelativePath}).
		Save(ctx); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, err
	}
	return makeView(record, principal.ProjectPublicID, node), nil
}

func (s *Service) resolveWorkspace(ctx context.Context, principal agentauth.Principal, selector string) (*ent.Project, *ent.NodeProjectAccess, *ent.SelfHostedNode, error) {
	projectRecord, err := s.client.Project.Query().Where(
		project.IDEQ(principal.ProjectID), project.TenantIDEQ(principal.TenantID), project.StatusEQ(project.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil, nil, ErrTrustedWorkspace
	}
	if err != nil {
		return nil, nil, nil, err
	}
	accesses, err := s.client.NodeProjectAccess.Query().Where(
		nodeprojectaccess.ProjectIDEQ(projectRecord.ID), nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive),
		nodeprojectaccess.ExecutionPolicyEQ(nodeprojectaccess.ExecutionPolicyTrustedWorkspace), nodeprojectaccess.WorkspacePathNotNil(),
		nodeprojectaccess.HasNodeWith(selfhostednode.TenantIDEQ(principal.TenantID), selfhostednode.StatusEQ(selfhostednode.StatusActive)),
	).WithNode().All(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	selector = strings.TrimSpace(selector)
	matches := make([]*ent.NodeProjectAccess, 0, len(accesses))
	for _, access := range accesses {
		node, edgeErr := access.Edges.NodeOrErr()
		if edgeErr != nil {
			return nil, nil, nil, edgeErr
		}
		if selector == "" || strings.EqualFold(selector, node.Label) || strings.EqualFold(selector, node.PublicID.String()) {
			matches = append(matches, access)
		}
	}
	if len(matches) == 0 {
		return nil, nil, nil, ErrTrustedWorkspace
	}
	if len(matches) > 1 {
		labels := make([]string, 0, len(matches))
		for _, access := range matches {
			node, _ := access.Edges.NodeOrErr()
			labels = append(labels, node.Label)
		}
		sort.Strings(labels)
		return nil, nil, nil, fmt.Errorf("%w: %s", ErrWorkspaceChoice, strings.Join(labels, ", "))
	}
	node, err := matches[0].Edges.NodeOrErr()
	return projectRecord, matches[0], node, err
}

func NormalizeRelativePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || strings.Contains(value, "\\") || path.IsAbs(value) {
		return "", invalid("relative_path must be a UTF-8 path below the approved workspace root")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", invalid("relative_path must not contain control characters")
		}
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned != value || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", invalid("relative_path must be normalized and must not contain traversal")
	}
	return cleaned, nil
}

func DatasetEnvironmentVariable(name string) string {
	value := strings.ToUpper(strings.TrimSpace(name))
	value = strings.Trim(nonEnvironmentCharacter.ReplaceAllString(value, "_"), "_")
	return "GEMCP_DATASET_" + value
}

func ContainerPath(relativePath string) string { return "/gemcp/workspace/" + relativePath }

func makeView(record *ent.WorkspaceDataset, projectID string, node *ent.SelfHostedNode) View {
	return View{
		ID: record.PublicID.String(), ProjectID: projectID, NodeID: node.PublicID.String(), NodeLabel: node.Label,
		Name: record.Name, RelativePath: record.RelativePath, ContainerPath: ContainerPath(record.RelativePath),
		EnvironmentVariable: record.EnvironmentVariable, Status: string(record.Status),
	}
}
