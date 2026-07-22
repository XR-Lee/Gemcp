package selfhosted

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/google/uuid"
)

type RuntimeConfigInput struct {
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	GPUNames    []string `json:"gpu_names"`
	CPULimit    int      `json:"cpu_limit"`
	MemoryGB    int      `json:"memory_gb"`
	MakeDefault bool     `json:"make_default"`
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
