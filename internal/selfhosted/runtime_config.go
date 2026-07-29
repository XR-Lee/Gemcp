package selfhosted

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/google/uuid"
)

const (
	trustedWorkspaceRecipePrefix = "trusted-workspace:"
	workspaceImagePlaceholder    = "workspace:any-public-image"
)

var workspaceNameCharacters = regexp.MustCompile(`[^a-z0-9]+`)

type RuntimeConfigInput struct {
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	GPUNames    []string `json:"gpu_names"`
	CPULimit    int      `json:"cpu_limit"`
	MemoryGB    int      `json:"memory_gb"`
	MakeDefault bool     `json:"make_default"`
}

type TrustedWorkspaceInput struct {
	NodeID        string `json:"node_id"`
	WorkspacePath string `json:"workspace_path"`
	MakeDefault   bool   `json:"make_default"`
}

type RuntimeEnvironmentView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Image     string `json:"image"`
	IsDefault bool   `json:"is_default"`
}

type RuntimeProfileView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	GPUNames  []string `json:"gpu_names"`
	CPULimit  int      `json:"cpu_limit"`
	MemoryGB  int      `json:"memory_gb"`
	IsDefault bool     `json:"is_default"`
}

type RuntimeConfigView struct {
	Environment RuntimeEnvironmentView `json:"environment"`
	Profile     RuntimeProfileView     `json:"resource_profile"`
}

type RuntimeConfigList struct {
	Environments []RuntimeEnvironmentView `json:"environments"`
	Profiles     []RuntimeProfileView     `json:"resource_profiles"`
	Workspaces   []TrustedWorkspaceView   `json:"trusted_workspaces"`
}

type TrustedWorkspaceView struct {
	NodeID            string   `json:"node_id"`
	NodeLabel         string   `json:"node_label"`
	WorkspacePath     string   `json:"workspace_path"`
	EnvironmentID     string   `json:"environment_id"`
	EnvironmentName   string   `json:"environment_name"`
	ResourceProfileID string   `json:"resource_profile_id"`
	GPUName           string   `json:"gpu_name"`
	CPULimit          int      `json:"cpu_limit"`
	MemoryGB          int      `json:"memory_gb"`
	SuccessfulImages  []string `json:"successful_images"`
	NodeReady         bool     `json:"node_ready"`
}

type TrustedWorkspaceDisableResult struct {
	NodeID   string `json:"node_id"`
	Disabled bool   `json:"disabled"`
}

func (s *Service) EnableTrustedWorkspace(ctx context.Context, tenantID int, actorID, projectPublicID string, input TrustedWorkspaceInput) (TrustedWorkspaceView, error) {
	var result TrustedWorkspaceView
	if !s.config.Enabled {
		return result, ErrDisabled
	}
	projectID, projectErr := uuid.Parse(strings.TrimSpace(projectPublicID))
	nodeID, nodeErr := uuid.Parse(strings.TrimSpace(input.NodeID))
	workspacePath, pathErr := normalizedWorkspacePath(input.WorkspacePath)
	if projectErr != nil || nodeErr != nil || pathErr != nil {
		return result, invalid("node_id and a safe absolute workspace_path are required")
	}
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	projectRecord, err := tx.Project.Query().Where(project.PublicIDEQ(projectID), project.TenantIDEQ(tenantID), project.StatusNEQ(project.StatusArchived)).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrProject
	}
	if err != nil {
		return result, err
	}
	nodeRecord, err := tx.SelfHostedNode.Query().Where(selfhostednode.PublicIDEQ(nodeID), selfhostednode.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return result, invalid("node_id must identify a Node in this organization")
	}
	if err != nil {
		return result, err
	}
	access, err := tx.NodeProjectAccess.Query().Where(
		nodeprojectaccess.NodeIDEQ(nodeRecord.ID), nodeprojectaccess.ProjectIDEQ(projectRecord.ID), nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, invalid("the Node must be authorized for this Project before enabling a trusted workspace")
	}
	if err != nil {
		return result, err
	}
	gpuName, cpuLimit, memoryGB, err := workspaceHardware(nodeRecord.Capabilities)
	if err != nil {
		return result, err
	}
	recipeRef := trustedWorkspaceRecipePrefix + nodeRecord.PublicID.String()
	environmentRecord, err := tx.Environment.Query().Where(environment.ProjectIDEQ(projectRecord.ID), environment.RecipeRefEQ(recipeRef)).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return result, err
	}
	name := workspaceRuntimeName(nodeRecord.Label, nodeRecord.PublicID)
	if ent.IsNotFound(err) {
		environmentRecord, err = tx.Environment.Create().SetProjectID(projectRecord.ID).SetBackend(environment.BackendSelfHosted).
			SetName(name).SetImageUUID(workspaceImagePlaceholder).SetRecipeRef(recipeRef).SetIsDefault(input.MakeDefault).Save(ctx)
	} else {
		name = environmentRecord.Name
		environmentRecord, err = environmentRecord.Update().SetStatus(environment.StatusApproved).SetIsDefault(input.MakeDefault).Save(ctx)
	}
	if err != nil {
		return result, fmt.Errorf("save trusted workspace Environment: %w", err)
	}
	profileRecord, err := tx.ResourceProfile.Query().Where(
		resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.NameEQ(name), resourceprofile.BackendEQ(resourceprofile.BackendSelfHosted),
	).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return result, err
	}
	if ent.IsNotFound(err) {
		profileRecord, err = tx.ResourceProfile.Create().SetProjectID(projectRecord.ID).SetBackend(resourceprofile.BackendSelfHosted).
			SetName(name).SetRegion("trusted_workspace").SetGpuNames([]string{gpuName}).SetGpuNum(1).SetCudaFrom(1).SetCudaTo(1).
			SetCPUFrom(1).SetCPUTo(cpuLimit).SetMemoryFromGB(1).SetMemoryToGB(memoryGB).
			SetPriceFromMilli(0).SetPriceToMilli(0).SetIsDefault(input.MakeDefault).Save(ctx)
	} else {
		profileRecord, err = profileRecord.Update().SetStatus(resourceprofile.StatusActive).SetGpuNames([]string{gpuName}).
			SetCPUTo(cpuLimit).SetMemoryToGB(memoryGB).SetIsDefault(input.MakeDefault).Save(ctx)
	}
	if err != nil {
		return result, fmt.Errorf("save trusted workspace Resource Profile: %w", err)
	}
	if input.MakeDefault {
		if _, err := tx.Environment.Update().Where(environment.ProjectIDEQ(projectRecord.ID), environment.IDNEQ(environmentRecord.ID), environment.IsDefaultEQ(true)).SetIsDefault(false).Save(ctx); err != nil {
			return result, err
		}
		if _, err := tx.ResourceProfile.Update().Where(resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.IDNEQ(profileRecord.ID), resourceprofile.IsDefaultEQ(true)).SetIsDefault(false).Save(ctx); err != nil {
			return result, err
		}
	}
	access, err = access.Update().SetExecutionPolicy(nodeprojectaccess.ExecutionPolicyTrustedWorkspace).SetWorkspacePath(workspacePath).Save(ctx)
	if err != nil {
		return result, err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("self_hosted.trusted_workspace_enabled").SetTargetType("self_hosted_node").SetTargetID(nodeRecord.PublicID.String()).
		SetMetadata(map[string]any{"project_id": projectRecord.PublicID.String(), "workspace_path": workspacePath, "environment_id": environmentRecord.PublicID.String(), "resource_profile_id": profileRecord.PublicID.String(), "make_default": input.MakeDefault}).Save(ctx); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return trustedWorkspaceView(nodeRecord, access, environmentRecord, profileRecord), nil
}

func (s *Service) DisableTrustedWorkspace(ctx context.Context, tenantID int, actorID, projectPublicID, nodePublicID string) (TrustedWorkspaceDisableResult, error) {
	var result TrustedWorkspaceDisableResult
	if !s.config.Enabled {
		return result, ErrDisabled
	}
	projectID, projectErr := uuid.Parse(strings.TrimSpace(projectPublicID))
	nodeID, nodeErr := uuid.Parse(strings.TrimSpace(nodePublicID))
	if projectErr != nil || nodeErr != nil {
		return result, ErrProject
	}
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	projectRecord, err := tx.Project.Query().Where(project.PublicIDEQ(projectID), project.TenantIDEQ(tenantID), project.StatusNEQ(project.StatusArchived)).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrProject
	}
	if err != nil {
		return result, err
	}
	nodeRecord, err := tx.SelfHostedNode.Query().Where(selfhostednode.PublicIDEQ(nodeID), selfhostednode.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return result, invalid("node_id must identify a Node in this organization")
	}
	if err != nil {
		return result, err
	}
	access, err := tx.NodeProjectAccess.Query().Where(
		nodeprojectaccess.NodeIDEQ(nodeRecord.ID), nodeprojectaccess.ProjectIDEQ(projectRecord.ID), nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive),
		nodeprojectaccess.ExecutionPolicyEQ(nodeprojectaccess.ExecutionPolicyTrustedWorkspace),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return result, invalid("trusted workspace is not enabled for this Node and Project")
	}
	if err != nil {
		return result, err
	}
	busy, err := tx.NodeAssignment.Query().Where(
		nodeassignment.NodeIDEQ(nodeRecord.ID), nodeassignment.ProjectIDEQ(projectRecord.ID),
		nodeassignment.StateIn(nodeassignment.StateStarting, nodeassignment.StateRunning, nodeassignment.StateStopping, nodeassignment.StateCollecting),
	).Exist(ctx)
	if err != nil {
		return result, err
	}
	if busy {
		return result, invalid("trusted workspace cannot be disabled while the Node has an active Assignment")
	}
	if _, err := access.Update().SetExecutionPolicy(nodeprojectaccess.ExecutionPolicyStrict).ClearWorkspacePath().Save(ctx); err != nil {
		return result, err
	}
	recipeRef := trustedWorkspaceRecipePrefix + nodeRecord.PublicID.String()
	environmentRecord, err := tx.Environment.Query().Where(environment.ProjectIDEQ(projectRecord.ID), environment.RecipeRefEQ(recipeRef)).Only(ctx)
	if err == nil {
		if _, err := environmentRecord.Update().SetStatus(environment.StatusDisabled).SetIsDefault(false).Save(ctx); err != nil {
			return result, err
		}
		if _, err := tx.ResourceProfile.Update().Where(
			resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.NameEQ(environmentRecord.Name), resourceprofile.BackendEQ(resourceprofile.BackendSelfHosted),
		).SetStatus(resourceprofile.StatusDisabled).SetIsDefault(false).Save(ctx); err != nil {
			return result, err
		}
	} else if !ent.IsNotFound(err) {
		return result, err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("self_hosted.trusted_workspace_disabled").SetTargetType("self_hosted_node").SetTargetID(nodeRecord.PublicID.String()).
		SetMetadata(map[string]any{"project_id": projectRecord.PublicID.String()}).Save(ctx); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return TrustedWorkspaceDisableResult{NodeID: nodeRecord.PublicID.String(), Disabled: true}, nil
}

func (s *Service) CreateRuntimeConfig(ctx context.Context, tenantID int, actorID, projectPublicID string, input RuntimeConfigInput) (RuntimeConfigView, error) {
	var result RuntimeConfigView
	if !s.config.Enabled {
		return result, ErrDisabled
	}
	projectID, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return result, ErrProject
	}
	name := strings.TrimSpace(input.Name)
	image := strings.TrimSpace(input.Image)
	gpuNames, err := normalizedGPUNames(input.GPUNames)
	if name == "" || len(name) > 120 || !pinnedImagePattern.MatchString(image) || err != nil || input.CPULimit <= 0 || input.CPULimit > 1024 || input.MemoryGB <= 0 || input.MemoryGB > 4096 {
		return result, invalid("runtime name, digest-pinned image, GPU names, CPU limit, and memory are required")
	}
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	projectRecord, err := tx.Project.Query().Where(project.PublicIDEQ(projectID), project.TenantIDEQ(tenantID), project.StatusNEQ(project.StatusArchived)).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrProject
	}
	if err != nil {
		return result, err
	}
	if input.MakeDefault {
		if _, err := tx.Environment.Update().Where(environment.ProjectIDEQ(projectRecord.ID), environment.IsDefaultEQ(true)).SetIsDefault(false).Save(ctx); err != nil {
			return result, err
		}
		if _, err := tx.ResourceProfile.Update().Where(resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.IsDefaultEQ(true)).SetIsDefault(false).Save(ctx); err != nil {
			return result, err
		}
	}
	environmentRecord, err := tx.Environment.Create().SetProjectID(projectRecord.ID).SetBackend(environment.BackendSelfHosted).
		SetName(name).SetImageUUID(image).SetIsDefault(input.MakeDefault).Save(ctx)
	if err != nil {
		return result, err
	}
	profileRecord, err := tx.ResourceProfile.Create().SetProjectID(projectRecord.ID).SetBackend(resourceprofile.BackendSelfHosted).
		SetName(name).SetRegion(Backend).SetGpuNames(gpuNames).SetGpuNum(1).SetCudaFrom(1).SetCudaTo(1).
		SetCPUFrom(1).SetCPUTo(input.CPULimit).SetMemoryFromGB(1).SetMemoryToGB(input.MemoryGB).
		SetPriceFromMilli(0).SetPriceToMilli(0).SetIsDefault(input.MakeDefault).Save(ctx)
	if err != nil {
		return result, err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("self_hosted.runtime_config_created").SetTargetType("project").SetTargetID(projectRecord.PublicID.String()).
		SetMetadata(map[string]any{"environment_id": environmentRecord.PublicID.String(), "resource_profile_id": profileRecord.PublicID.String(), "image": image, "make_default": input.MakeDefault}).Save(ctx); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return runtimeConfigView(environmentRecord, profileRecord), nil
}

func (s *Service) ListRuntimeConfigs(ctx context.Context, tenantID int, projectPublicID string) (RuntimeConfigList, error) {
	var result RuntimeConfigList
	if !s.config.Enabled {
		return result, ErrDisabled
	}
	id, err := uuid.Parse(strings.TrimSpace(projectPublicID))
	if err != nil {
		return result, ErrProject
	}
	projectRecord, err := s.client.Project.Query().Where(project.PublicIDEQ(id), project.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrProject
	}
	if err != nil {
		return result, err
	}
	environments, err := s.client.Environment.Query().Where(
		environment.ProjectIDEQ(projectRecord.ID), environment.BackendEQ(environment.BackendSelfHosted),
	).Order(ent.Asc(environment.FieldName)).All(ctx)
	if err != nil {
		return result, err
	}
	profiles, err := s.client.ResourceProfile.Query().Where(
		resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.BackendEQ(resourceprofile.BackendSelfHosted),
	).Order(ent.Asc(resourceprofile.FieldName)).All(ctx)
	if err != nil {
		return result, err
	}
	result.Environments = make([]RuntimeEnvironmentView, 0, len(environments))
	for _, record := range environments {
		result.Environments = append(result.Environments, runtimeEnvironmentView(record))
	}
	result.Profiles = make([]RuntimeProfileView, 0, len(profiles))
	for _, record := range profiles {
		result.Profiles = append(result.Profiles, runtimeProfileView(record))
	}
	environmentsByRecipe := make(map[string]*ent.Environment, len(environments))
	profilesByName := make(map[string]*ent.ResourceProfile, len(profiles))
	for _, record := range environments {
		if record.RecipeRef != "" {
			environmentsByRecipe[record.RecipeRef] = record
		}
	}
	for _, record := range profiles {
		profilesByName[record.Name] = record
	}
	accesses, err := s.client.NodeProjectAccess.Query().Where(
		nodeprojectaccess.ProjectIDEQ(projectRecord.ID), nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive),
		nodeprojectaccess.ExecutionPolicyEQ(nodeprojectaccess.ExecutionPolicyTrustedWorkspace), nodeprojectaccess.WorkspacePathNotNil(),
	).WithNode().Order(ent.Asc(nodeprojectaccess.FieldID)).All(ctx)
	if err != nil {
		return result, err
	}
	result.Workspaces = make([]TrustedWorkspaceView, 0, len(accesses))
	for _, access := range accesses {
		node, nodeErr := access.Edges.NodeOrErr()
		if nodeErr != nil {
			return result, nodeErr
		}
		environmentRecord := environmentsByRecipe[trustedWorkspaceRecipePrefix+node.PublicID.String()]
		if environmentRecord == nil {
			continue
		}
		profileRecord := profilesByName[environmentRecord.Name]
		if profileRecord == nil {
			continue
		}
		result.Workspaces = append(result.Workspaces, trustedWorkspaceView(node, access, environmentRecord, profileRecord))
	}
	return result, nil
}

func runtimeConfigView(environment *ent.Environment, profile *ent.ResourceProfile) RuntimeConfigView {
	return RuntimeConfigView{Environment: runtimeEnvironmentView(environment), Profile: runtimeProfileView(profile)}
}

func runtimeEnvironmentView(record *ent.Environment) RuntimeEnvironmentView {
	return RuntimeEnvironmentView{ID: record.PublicID.String(), Name: record.Name, Image: record.ImageUUID, IsDefault: record.IsDefault}
}

func runtimeProfileView(record *ent.ResourceProfile) RuntimeProfileView {
	return RuntimeProfileView{
		ID: record.PublicID.String(), Name: record.Name, GPUNames: append([]string(nil), record.GpuNames...),
		CPULimit: record.CPUTo, MemoryGB: record.MemoryToGB, IsDefault: record.IsDefault,
	}
}

func trustedWorkspaceView(node *ent.SelfHostedNode, access *ent.NodeProjectAccess, environment *ent.Environment, profile *ent.ResourceProfile) TrustedWorkspaceView {
	workspacePath := ""
	if access.WorkspacePath != nil {
		workspacePath = *access.WorkspacePath
	}
	gpuName := ""
	if len(profile.GpuNames) > 0 {
		gpuName = profile.GpuNames[0]
	}
	return TrustedWorkspaceView{
		NodeID: node.PublicID.String(), NodeLabel: node.Label, WorkspacePath: workspacePath,
		EnvironmentID: environment.PublicID.String(), EnvironmentName: environment.Name, ResourceProfileID: profile.PublicID.String(),
		GPUName: gpuName, CPULimit: profile.CPUTo, MemoryGB: profile.MemoryToGB,
		SuccessfulImages: append([]string{}, access.SuccessfulImages...),
		NodeReady:        node.Status == selfhostednode.StatusActive && node.ObservedState == selfhostednode.ObservedStateOnline && supportsWorkspaceMode(node.Capabilities),
	}
}

func normalizedWorkspacePath(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 4096 || !filepath.IsAbs(value) || strings.ContainsAny(value, ",\x00") {
		return "", invalid("workspace_path must be a safe absolute path")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", invalid("workspace_path contains a control character")
		}
	}
	clean := filepath.Clean(value)
	if clean != value {
		return "", invalid("workspace_path must be normalized without trailing separators or parent traversal")
	}
	for _, protected := range []string{"/", "/boot", "/dev", "/etc", "/proc", "/run", "/sys", "/usr", "/var/lib/docker"} {
		if clean == protected || (protected != "/" && strings.HasPrefix(clean, protected+string(filepath.Separator))) {
			return "", invalid("workspace_path cannot expose a protected host root")
		}
	}
	return clean, nil
}

func workspaceHardware(capabilities map[string]any) (string, int, int, error) {
	var gpus []any
	switch values := capabilities["gpus"].(type) {
	case []any:
		gpus = values
	case []map[string]any:
		gpus = make([]any, len(values))
		for index, value := range values {
			gpus[index] = value
		}
	}
	if len(gpus) != 1 {
		return "", 0, 0, invalid("trusted workspace mode currently requires exactly one reported GPU")
	}
	gpu, ok := gpus[0].(map[string]any)
	if !ok {
		return "", 0, 0, invalid("the Node GPU inventory is invalid")
	}
	gpuName, _ := gpu["name"].(string)
	cpuCount := capabilityInt(capabilities["cpu_count"])
	memoryBytes := capabilityInt64(capabilities["memory_bytes"])
	if strings.TrimSpace(gpuName) == "" || cpuCount <= 0 || memoryBytes < 2<<30 {
		return "", 0, 0, invalid("the Node has not reported usable GPU, CPU, and memory inventory")
	}
	cpuLimit := cpuCount - 2
	if cpuLimit < 1 {
		cpuLimit = 1
	}
	memoryGB := int(memoryBytes>>30) - 2
	if memoryGB < 1 {
		memoryGB = 1
	}
	return strings.TrimSpace(gpuName), cpuLimit, memoryGB, nil
}

func capabilityInt(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}

func capabilityInt64(value any) int64 {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int64:
		return number
	case float64:
		return int64(number)
	default:
		return 0
	}
}

func supportsWorkspaceMode(capabilities map[string]any) bool {
	switch values := capabilities["workspace_modes"].(type) {
	case []any:
		for _, value := range values {
			if value == "trusted_rw" {
				return true
			}
		}
	case []string:
		for _, value := range values {
			if value == "trusted_rw" {
				return true
			}
		}
	}
	return false
}

func workspaceRuntimeName(label string, publicID uuid.UUID) string {
	name := strings.Trim(workspaceNameCharacters.ReplaceAllString(strings.ToLower(strings.TrimSpace(label)), "-"), "-")
	if name == "" {
		name = "node"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return "workspace-" + name + "-" + publicID.String()[:8]
}

func normalizedGPUNames(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 16 {
		return nil, invalid("gpu_names must contain between 1 and 16 unique names")
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		key := strings.ToLower(value)
		if value == "" || len(value) > 120 || seen[key] {
			return nil, invalid("gpu_names must contain between 1 and 16 unique names")
		}
		seen[key] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}
