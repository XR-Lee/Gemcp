package experiment

import (
	"context"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
	"github.com/XR-Lee/Gemcp/ent/projectworkload"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/workload"
)

const maxProjectWorkloads = 32

type SaveWorkloadInput struct {
	ExperimentID string `json:"experiment_id"`
	Name         string `json:"name"`
}

type ProjectWorkloadView struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	ManifestYAML       string    `json:"manifest_yaml"`
	Entrypoint         []string  `json:"entrypoint"`
	RuntimePreset      string    `json:"runtime_preset,omitempty"`
	Dataset            string    `json:"dataset,omitempty"`
	WorkingDirectory   string    `json:"working_directory,omitempty"`
	SourceExperimentID string    `json:"source_experiment_id"`
	CreatedAt          time.Time `json:"created_at"`
}

type WorkloadPreview struct {
	Name         string `json:"name"`
	ManifestYAML string `json:"manifest_yaml"`
}

type WorkloadListResult struct {
	Workloads []ProjectWorkloadView `json:"workloads"`
}

func (s *Service) OwnerPreviewWorkload(ctx context.Context, tenantID int, projectPublicID string, input SaveWorkloadInput) (WorkloadPreview, error) {
	_, experimentRecord, proposal, err := s.savableOneShot(ctx, tenantID, projectPublicID, input)
	if err != nil {
		return WorkloadPreview{}, err
	}
	_, raw, err := draftOneShot(input.Name, experimentRecord, proposal)
	if err != nil {
		return WorkloadPreview{}, err
	}
	return WorkloadPreview{Name: strings.TrimSpace(input.Name), ManifestYAML: string(raw)}, nil
}

func (s *Service) OwnerSaveWorkload(ctx context.Context, tenantID int, actorID, projectPublicID string, input SaveWorkloadInput) (ProjectWorkloadView, error) {
	principal, experimentRecord, proposal, err := s.savableOneShot(ctx, tenantID, projectPublicID, input)
	if err != nil {
		return ProjectWorkloadView{}, err
	}
	name := strings.TrimSpace(input.Name)
	manifest, raw, err := draftOneShot(name, experimentRecord, proposal)
	if err != nil {
		return ProjectWorkloadView{}, err
	}
	count, err := s.client.ProjectWorkload.Query().Where(projectworkload.ProjectIDEQ(principal.ProjectID)).Count(ctx)
	if err != nil {
		return ProjectWorkloadView{}, err
	}
	if count >= maxProjectWorkloads {
		return ProjectWorkloadView{}, ErrWorkloadLimit
	}
	existingName, err := s.client.ProjectWorkload.Query().Where(
		projectworkload.ProjectIDEQ(principal.ProjectID), projectworkload.NameEQ(name),
	).Exist(ctx)
	if err != nil {
		return ProjectWorkloadView{}, err
	}
	if existingName {
		return ProjectWorkloadView{}, ErrWorkloadConflict
	}
	existingSource, err := s.client.ProjectWorkload.Query().Where(projectworkload.SourceExperimentIDEQ(experimentRecord.ID)).Exist(ctx)
	if err != nil {
		return ProjectWorkloadView{}, err
	}
	if existingSource {
		return ProjectWorkloadView{}, &ValidationError{Message: "this experiment is already saved as a Project workload"}
	}
	item := manifest.Workloads[name]
	create := s.client.ProjectWorkload.Create().
		SetTenantID(tenantID).SetProjectID(principal.ProjectID).SetSourceExperimentID(experimentRecord.ID).
		SetName(name).SetManifestYaml(string(raw)).SetEntrypoint(append([]string{}, item.Entrypoint...))
	if item.RuntimePreset != "" {
		create.SetRuntimePreset(item.RuntimePreset)
	}
	if len(item.Datasets) > 0 {
		create.SetDataset(item.Datasets[0])
	}
	if item.WorkingDirectory != "" {
		create.SetWorkingDirectory(item.WorkingDirectory)
	}
	record, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return ProjectWorkloadView{}, ErrWorkloadConflict
		}
		return ProjectWorkloadView{}, err
	}
	if _, auditErr := s.client.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType(auditevent.ActorTypeUser).SetActorID(strings.TrimSpace(actorID)).
		SetAction("project.workload_saved").SetTargetType("project_workload").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{
			"project_id": principal.ProjectPublicID, "experiment_id": experimentRecord.PublicID.String(),
			"name": name,
		}).Save(ctx); auditErr != nil {
		return ProjectWorkloadView{}, auditErr
	}
	return projectWorkloadView(record, experimentRecord.PublicID.String()), nil
}

func (s *Service) OwnerListWorkloads(ctx context.Context, tenantID int, projectPublicID string) (WorkloadListResult, error) {
	principal, err := s.ownerPrincipal(ctx, tenantID, projectPublicID)
	if err != nil {
		return WorkloadListResult{}, err
	}
	records, err := s.client.ProjectWorkload.Query().Where(projectworkload.ProjectIDEQ(principal.ProjectID)).
		Order(ent.Asc(projectworkload.FieldName)).All(ctx)
	if err != nil {
		return WorkloadListResult{}, err
	}
	experimentIDs := make([]int, 0, len(records))
	for _, record := range records {
		experimentIDs = append(experimentIDs, record.SourceExperimentID)
	}
	experiments := map[int]string{}
	if len(experimentIDs) > 0 {
		items, err := s.client.Experiment.Query().Where(entexperiment.IDIn(experimentIDs...)).All(ctx)
		if err != nil {
			return WorkloadListResult{}, err
		}
		for _, item := range items {
			experiments[item.ID] = item.PublicID.String()
		}
	}
	result := WorkloadListResult{Workloads: make([]ProjectWorkloadView, 0, len(records))}
	for _, record := range records {
		result.Workloads = append(result.Workloads, projectWorkloadView(record, experiments[record.SourceExperimentID]))
	}
	return result, nil
}

func (s *Service) savableOneShot(ctx context.Context, tenantID int, projectPublicID string, input SaveWorkloadInput) (principal agentauth.Principal, experimentRecord *ent.Experiment, proposal *ent.ExperimentProposal, err error) {
	owner, err := s.ownerPrincipal(ctx, tenantID, projectPublicID)
	if err != nil {
		return owner, nil, nil, err
	}
	experimentRecord, err = s.getRecord(ctx, owner.ProjectID, input.ExperimentID)
	if err != nil {
		return owner, nil, nil, err
	}
	if experimentRecord.State != "succeeded" {
		return owner, nil, nil, &ValidationError{Message: "only a succeeded one-shot experiment can be saved as a workload"}
	}
	if string(experimentRecord.ExecutionMode) != "argv" || len(experimentRecord.Argv) == 0 {
		return owner, nil, nil, &ValidationError{Message: "only argv one-shot experiments can be saved as a workload"}
	}
	proposal, err = s.client.ExperimentProposal.Query().Where(experimentproposal.ExperimentIDEQ(experimentRecord.ID)).Only(ctx)
	if ent.IsNotFound(err) {
		return owner, nil, nil, &ValidationError{Message: "only a prepared one-shot proposal can be saved as a workload"}
	}
	if err != nil {
		return owner, nil, nil, err
	}
	if strings.EqualFold(strings.TrimSpace(proposal.RuntimePreset), "provision") {
		return owner, nil, nil, &ValidationError{Message: "provision experiments cannot be saved as a workload"}
	}
	if snapshotString(proposal.ProjectSnapshot, "workload") != "" {
		return owner, nil, nil, &ValidationError{Message: "this experiment already used a named workload"}
	}
	return owner, experimentRecord, proposal, nil
}

func draftOneShot(name string, experimentRecord *ent.Experiment, proposal *ent.ExperimentProposal) (workload.Manifest, []byte, error) {
	cwd := snapshotString(proposal.ProjectSnapshot, "working_directory")
	if cwd == "" {
		cwd = snapshotString(proposal.EnvironmentSnapshot, "working_directory")
	}
	manifest, raw, err := workload.DraftFromArgv(workload.DraftInput{
		Name:             name,
		Argv:             experimentRecord.Argv,
		RuntimePreset:    proposal.RuntimePreset,
		Dataset:          snapshotString(proposal.ProjectSnapshot, "dataset"),
		WorkingDirectory: cwd,
	})
	if err != nil {
		return workload.Manifest{}, nil, &ValidationError{Message: err.Error()}
	}
	return manifest, raw, nil
}

func projectWorkloadView(record *ent.ProjectWorkload, experimentPublicID string) ProjectWorkloadView {
	return ProjectWorkloadView{
		ID: record.PublicID.String(), Name: record.Name, ManifestYAML: record.ManifestYaml,
		Entrypoint: append([]string{}, record.Entrypoint...), RuntimePreset: record.RuntimePreset,
		Dataset: record.Dataset, WorkingDirectory: record.WorkingDirectory,
		SourceExperimentID: experimentPublicID, CreatedAt: record.CreatedAt,
	}
}
