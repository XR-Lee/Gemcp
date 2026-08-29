package experiment

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/repository"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/ent/workspacedataset"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/XR-Lee/Gemcp/internal/executioncmd"
	"github.com/XR-Lee/Gemcp/internal/sshcloud"
	"github.com/google/uuid"
)

var errProposalRaced = errors.New("proposal submission raced with another transaction")

func (s *Service) SubmitPrepared(ctx context.Context, principal agentauth.Principal, input SubmitPreparedInput) (SubmitPreparedResult, error) {
	if !principal.HasScope("submit") {
		return SubmitPreparedResult{}, ErrForbidden
	}
	proposalID, err := uuid.Parse(strings.TrimSpace(input.ProposalID))
	if err != nil {
		return SubmitPreparedResult{}, ErrProposalNotFound
	}
	digest := strings.ToLower(strings.TrimSpace(input.ConfirmationDigest))
	if len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		return SubmitPreparedResult{}, &ValidationError{Message: "confirmation_digest must be the exact sha256 digest returned by prepare_experiment"}
	}
	record, err := s.client.ExperimentProposal.Query().Where(
		experimentproposal.PublicIDEQ(proposalID), experimentproposal.ProjectIDEQ(principal.ProjectID),
		experimentproposal.AgentTokenIDEQ(principal.TokenID),
	).WithExperiment().Only(ctx)
	if ent.IsNotFound(err) {
		return SubmitPreparedResult{}, ErrProposalNotFound
	}
	if err != nil {
		return SubmitPreparedResult{}, err
	}
	if !hmac.Equal([]byte(digest), []byte(record.ConfirmationDigest)) {
		return SubmitPreparedResult{}, ErrProposalChanged
	}
	if record.Status == experimentproposal.StatusSubmitted {
		experimentRecord, err := record.Edges.ExperimentOrErr()
		if err != nil {
			return SubmitPreparedResult{}, err
		}
		return s.bindPreparedGraph(ctx, principal, record.ProjectSnapshot, SubmitPreparedResult{Experiment: makeView(experimentRecord), Idempotent: true})
	}
	if !s.now().UTC().Before(record.ExpiresAt) {
		return SubmitPreparedResult{}, ErrProposalExpired
	}
	if !storedProposalEligible(record.Checks) {
		return SubmitPreparedResult{}, ErrProposalBlocked
	}
	resolved, err := s.currentProposal(ctx, principal, record)
	if err != nil {
		return SubmitPreparedResult{}, err
	}
	if !hmac.Equal([]byte(proposalDigest(resolved)), []byte(record.ConfirmationDigest)) {
		return SubmitPreparedResult{}, ErrProposalChanged
	}
	resolved.checks = s.proposalChecks(ctx, resolved)
	if !proposalChecksEligible(resolved.checks) {
		return SubmitPreparedResult{}, ErrProposalBlocked
	}
	for attempt := 0; attempt < 8; attempt++ {
		result, err := s.createPreparedExperiment(ctx, principal, record.ID, digest)
		if err == nil {
			return s.bindPreparedGraph(ctx, principal, record.ProjectSnapshot, result)
		}
		if !isRetryableTransaction(err) && !ent.IsConstraintError(err) && !errors.Is(err, errProposalRaced) {
			return SubmitPreparedResult{}, err
		}
		existing, lookupErr := s.client.ExperimentProposal.Query().Where(
			experimentproposal.IDEQ(record.ID), experimentproposal.ProjectIDEQ(principal.ProjectID),
			experimentproposal.AgentTokenIDEQ(principal.TokenID),
		).WithExperiment().Only(ctx)
		if lookupErr == nil && existing.Status == experimentproposal.StatusSubmitted {
			experimentRecord, edgeErr := existing.Edges.ExperimentOrErr()
			if edgeErr != nil {
				return SubmitPreparedResult{}, edgeErr
			}
			return s.bindPreparedGraph(ctx, principal, existing.ProjectSnapshot, SubmitPreparedResult{Experiment: makeView(experimentRecord), Idempotent: true})
		}
		if lookupErr != nil && !ent.IsNotFound(lookupErr) {
			return SubmitPreparedResult{}, lookupErr
		}
		if attempt < 7 {
			select {
			case <-ctx.Done():
				return SubmitPreparedResult{}, ctx.Err()
			case <-time.After(time.Millisecond << attempt):
			}
		}
	}
	return SubmitPreparedResult{}, fmt.Errorf("prepared experiment submission transaction did not converge")
}

func (s *Service) currentProposal(ctx context.Context, principal agentauth.Principal, record *ent.ExperimentProposal) (proposalResolved, error) {
	var result proposalResolved
	projectRecord, err := s.client.Project.Query().Where(
		project.IDEQ(record.ProjectID), project.TenantIDEQ(principal.TenantID), project.StatusEQ(project.StatusActive),
	).Only(ctx)
	if err != nil {
		return result, proposalDriftError(err)
	}
	var repositoryRecord *ent.Repository
	if record.RepositoryID != nil {
		repositoryRecord, err = s.client.Repository.Query().Where(
			repository.IDEQ(*record.RepositoryID), repository.ProjectIDEQ(projectRecord.ID), repository.StatusEQ(repository.StatusActive),
		).Only(ctx)
		if err != nil {
			return result, proposalDriftError(err)
		}
	}
	environmentRecord, err := s.client.Environment.Query().Where(
		environment.IDEQ(record.EnvironmentID), environment.ProjectIDEQ(projectRecord.ID), environment.StatusEQ(environment.StatusApproved),
	).Only(ctx)
	if err != nil {
		return result, proposalDriftError(err)
	}
	profileRecord, err := s.client.ResourceProfile.Query().Where(
		resourceprofile.IDEQ(record.ResourceProfileID), resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.StatusEQ(resourceprofile.StatusActive),
	).Only(ctx)
	if err != nil || string(environmentRecord.Backend) != string(profileRecord.Backend) {
		return result, ErrProposalChanged
	}
	executionSpec, err := executioncmd.Argv(record.Argv)
	if err != nil {
		return result, ErrProposalChanged
	}
	reservation, err := proposalReservation(projectRecord, profileRecord, record.MaxRuntimeSeconds)
	if err != nil {
		return result, ErrProposalChanged
	}
	image, workspace, err := s.resolveProposalRuntime(ctx, projectRecord, environmentRecord, profileRecord, snapshotString(record.EnvironmentSnapshot, "image_uuid"), true)
	if err != nil {
		return result, ErrProposalChanged
	}
	// cwd and the ssh_* pins are populated by applyStoredProposalPins below.
	resolved := proposalResolved{
		id: record.PublicID, project: projectRecord, repository: repositoryRecord, environment: environmentRecord, profile: profileRecord,
		image: image, workspace: workspace,
		ref: record.RequestedRef, commitSHA: record.CommitSha, execution: executionSpec, preset: record.RuntimePreset,
		runtime: record.MaxRuntimeSeconds, reservation: reservation, expiresAt: record.ExpiresAt,
		fromNodeID: snapshotString(record.ProjectSnapshot, "from_node_id"), expectedMetric: snapshotString(record.ProjectSnapshot, "expected_metric"),
		dataset: snapshotString(record.ProjectSnapshot, "dataset"),
	}
	bindings, err := queryActiveDatasetBindings(ctx, s.client.DatasetBinding.Query(), projectRecord.ID, string(profileRecord.Backend))
	if err != nil {
		return result, err
	}
	resolved.bindings = bindings
	if err := applyStoredProposalPins(&resolved, record.EnvironmentSnapshot, record.ProjectSnapshot); err != nil {
		return result, err
	}
	return resolved, nil
}

func (s *Service) createPreparedExperiment(ctx context.Context, principal agentauth.Principal, proposalDatabaseID int, digest string) (SubmitPreparedResult, error) {
	var result SubmitPreparedResult
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	proposalRecord, err := tx.ExperimentProposal.Query().Where(
		experimentproposal.IDEQ(proposalDatabaseID), experimentproposal.ProjectIDEQ(principal.ProjectID),
		experimentproposal.AgentTokenIDEQ(principal.TokenID),
	).WithExperiment().Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrProposalNotFound
	}
	if err != nil {
		return result, err
	}
	if !hmac.Equal([]byte(digest), []byte(proposalRecord.ConfirmationDigest)) {
		return result, ErrProposalChanged
	}
	if proposalRecord.Status == experimentproposal.StatusSubmitted {
		experimentRecord, err := proposalRecord.Edges.ExperimentOrErr()
		if err != nil {
			return result, err
		}
		if err := tx.Commit(); err != nil {
			return result, err
		}
		return SubmitPreparedResult{Experiment: makeView(experimentRecord), Idempotent: true}, nil
	}
	if !s.now().UTC().Before(proposalRecord.ExpiresAt) {
		return result, ErrProposalExpired
	}
	projectRecord, err := tx.Project.Query().Where(
		project.IDEQ(proposalRecord.ProjectID), project.TenantIDEQ(principal.TenantID), project.StatusEQ(project.StatusActive),
	).Only(ctx)
	if err != nil {
		return result, proposalDriftError(err)
	}
	var repositoryRecord *ent.Repository
	if proposalRecord.RepositoryID != nil {
		repositoryRecord, err = tx.Repository.Query().Where(
			repository.IDEQ(*proposalRecord.RepositoryID), repository.ProjectIDEQ(projectRecord.ID), repository.StatusEQ(repository.StatusActive),
		).Only(ctx)
		if err != nil {
			return result, proposalDriftError(err)
		}
	}
	environmentRecord, err := tx.Environment.Query().Where(
		environment.IDEQ(proposalRecord.EnvironmentID), environment.ProjectIDEQ(projectRecord.ID), environment.StatusEQ(environment.StatusApproved),
	).Only(ctx)
	if err != nil {
		return result, proposalDriftError(err)
	}
	profileRecord, err := tx.ResourceProfile.Query().Where(
		resourceprofile.IDEQ(proposalRecord.ResourceProfileID), resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.StatusEQ(resourceprofile.StatusActive),
	).Only(ctx)
	if err != nil || string(environmentRecord.Backend) != string(profileRecord.Backend) {
		return result, ErrProposalChanged
	}
	executionSpec, err := executioncmd.Argv(proposalRecord.Argv)
	if err != nil {
		return result, ErrProposalChanged
	}
	reservation, err := proposalReservation(projectRecord, profileRecord, proposalRecord.MaxRuntimeSeconds)
	if err != nil {
		return result, ErrProposalChanged
	}
	image, workspace, err := proposalRuntimeInTransaction(ctx, tx, projectRecord, environmentRecord, profileRecord, proposalRecord.EnvironmentSnapshot)
	if err != nil {
		return result, ErrProposalChanged
	}
	current := proposalResolved{
		id: proposalRecord.PublicID, project: projectRecord, repository: repositoryRecord, environment: environmentRecord, profile: profileRecord,
		image: image, workspace: workspace,
		ref: proposalRecord.RequestedRef, commitSHA: proposalRecord.CommitSha, execution: executionSpec, preset: proposalRecord.RuntimePreset,
		runtime: proposalRecord.MaxRuntimeSeconds, reservation: reservation, expiresAt: proposalRecord.ExpiresAt,
		fromNodeID: snapshotString(proposalRecord.ProjectSnapshot, "from_node_id"), expectedMetric: snapshotString(proposalRecord.ProjectSnapshot, "expected_metric"),
		dataset: snapshotString(proposalRecord.ProjectSnapshot, "dataset"),
	}
	bindings, err := queryActiveDatasetBindings(ctx, tx.DatasetBinding.Query(), projectRecord.ID, string(profileRecord.Backend))
	if err != nil {
		return result, err
	}
	current.bindings = bindings
	if err := applyStoredProposalPins(&current, proposalRecord.EnvironmentSnapshot, proposalRecord.ProjectSnapshot); err != nil {
		return result, err
	}
	if !hmac.Equal([]byte(proposalDigest(current)), []byte(proposalRecord.ConfirmationDigest)) {
		return result, ErrProposalChanged
	}
	if reservation > projectRecord.MaxExperimentMilli {
		return result, ErrExperimentCap
	}
	period, err := budgetPeriod(s.now().UTC(), projectRecord.Timezone)
	if err != nil {
		return result, err
	}
	if !unmeteredBackend(profileRecord.Backend) {
		committed, err := ledgerTotal(ctx, tx, projectRecord.ID, period)
		if err != nil {
			return result, err
		}
		if reservation > projectRecord.MonthlyBudgetMilli || committed > projectRecord.MonthlyBudgetMilli-reservation {
			return result, ErrBudgetExceeded
		}
	}
	experimentID := uuid.New()
	outputPath := "/root/autodl-fs/projects/" + projectRecord.PublicID.String() + "/experiments/" + experimentID.String() + "/"
	selfHosted := unmeteredBackend(profileRecord.Backend)
	if selfHosted {
		outputPath = "managed://experiments/" + experimentID.String() + "/outputs"
	}
	create := tx.Experiment.Create().
		SetPublicID(experimentID).
		SetTenantID(principal.TenantID).
		SetProjectID(projectRecord.ID).
		SetAgentTokenID(principal.TokenID).
		SetEnvironmentID(environmentRecord.ID).
		SetResourceProfileID(profileRecord.ID).
		SetCommitSha(proposalRecord.CommitSha).
		SetExecutionMode("argv").
		SetArgv(executionSpec.Argv).
		SetCommand(executioncmd.DisplayArgv(executionSpec.Argv)).
		SetMaxRuntimeSeconds(proposalRecord.MaxRuntimeSeconds).
		SetTimeoutExtensionSeconds(projectRecord.TimeoutExtensionSeconds).
		SetTerminationGraceSeconds(projectRecord.TerminationGraceSeconds).
		SetRepositorySnapshot(repositorySnapshot(repositoryRecord, projectRecord.PublicID.String())).
		SetEnvironmentSnapshot(proposalRecord.EnvironmentSnapshot).
		SetResourceSnapshot(resourceSnapshot(profileRecord)).
		SetSecretNames([]string{}).
		SetOutputPath(outputPath).
		SetReservedCostMilli(reservation)
	if repositoryRecord != nil {
		create.SetRepositoryID(repositoryRecord.ID)
	}
	experimentRecord, err := create.Save(ctx)
	if err != nil {
		return result, err
	}
	description := "prepared experiment budget reservation"
	if selfHosted {
		description = "unmetered prepared experiment reservation"
	}
	if _, err := tx.BudgetEntry.Create().
		SetTenantID(principal.TenantID).SetProjectID(projectRecord.ID).SetExperimentID(experimentRecord.ID).
		SetPeriod(period).SetKind("reservation").SetAmountMilli(reservation).SetDescription(description).Save(ctx); err != nil {
		return result, err
	}
	now := s.now().UTC()
	updated, err := tx.ExperimentProposal.Update().Where(
		experimentproposal.IDEQ(proposalRecord.ID), experimentproposal.StatusEQ(experimentproposal.StatusPrepared),
	).SetStatus(experimentproposal.StatusSubmitted).SetExperimentID(experimentRecord.ID).SetSubmittedAt(now).Save(ctx)
	if err != nil {
		return result, err
	}
	if updated != 1 {
		return result, errProposalRaced
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(principal.TenantID).SetActorType("agent_token").SetActorID(principal.TokenPublicID).
		SetAction("experiment.submitted").SetTargetType("experiment").SetTargetID(experimentID.String()).
		SetMetadata(map[string]any{
			"project_id": projectRecord.PublicID.String(), "backend": profileRecord.Backend,
			"reserved_cost_milli": reservation, "proposal_id": proposalRecord.PublicID.String(),
			"confirmation_digest": proposalRecord.ConfirmationDigest, "execution_mode": "argv",
		}).Save(ctx); err != nil {
		return result, err
	}
	if _, err := tx.AuditEvent.Create().SetTenantID(principal.TenantID).SetActorType("agent_token").SetActorID(principal.TokenPublicID).
		SetAction("agent.activity").SetTargetType("project").SetTargetID(principal.ProjectPublicID).
		SetMetadata(map[string]any{
			"project_id": principal.ProjectPublicID, "phase": "monitoring", "proposal_id": proposalRecord.PublicID.String(),
			"experiment_id": experimentID.String(), "repository_remote": repositoryRemote(repositoryRecord), "ref": proposalRecord.RequestedRef,
		}).Save(ctx); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return SubmitPreparedResult{Experiment: makeView(experimentRecord)}, nil
}

func proposalRuntimeInTransaction(ctx context.Context, tx *ent.Tx, projectRecord *ent.Project, environmentRecord *ent.Environment, profileRecord *ent.ResourceProfile, snapshot map[string]any) (string, *proposalWorkspace, error) {
	image := snapshotString(snapshot, "image_uuid")
	recipeRef := strings.TrimSpace(environmentRecord.RecipeRef)
	if !strings.HasPrefix(recipeRef, proposalWorkspaceRecipePrefix) {
		if environmentRecord.Backend == environment.BackendSSHCloud {
			return sshcloud.HostImage, nil, nil
		}
		if image == "" || image != environmentRecord.ImageUUID {
			return "", nil, ErrProposalChanged
		}
		return image, nil, nil
	}
	if profileRecord.Backend != resourceprofile.BackendSelfHosted || profileRecord.Name != environmentRecord.Name || !proposalWorkspaceImage.MatchString(image) {
		return "", nil, ErrProposalChanged
	}
	nodePublicID, err := uuid.Parse(strings.TrimPrefix(recipeRef, proposalWorkspaceRecipePrefix))
	if err != nil || snapshotString(snapshot, "workspace_node_id") != nodePublicID.String() {
		return "", nil, ErrProposalChanged
	}
	access, err := tx.NodeProjectAccess.Query().Where(
		nodeprojectaccess.ProjectIDEQ(projectRecord.ID), nodeprojectaccess.StatusEQ(nodeprojectaccess.StatusActive),
		nodeprojectaccess.ExecutionPolicyEQ(nodeprojectaccess.ExecutionPolicyTrustedWorkspace), nodeprojectaccess.WorkspacePathNotNil(),
		nodeprojectaccess.HasNodeWith(selfhostednode.PublicIDEQ(nodePublicID), selfhostednode.TenantIDEQ(projectRecord.TenantID)),
	).WithNode().Only(ctx)
	if err != nil || access.WorkspacePath == nil || *access.WorkspacePath != snapshotString(snapshot, "workspace_path") {
		return "", nil, ErrProposalChanged
	}
	node, err := access.Edges.NodeOrErr()
	if err != nil || node.Label != snapshotString(snapshot, "workspace_node_label") {
		return "", nil, ErrProposalChanged
	}
	datasetRecords, err := tx.WorkspaceDataset.Query().Where(
		workspacedataset.ProjectIDEQ(projectRecord.ID), workspacedataset.NodeIDEQ(node.ID), workspacedataset.StatusEQ(workspacedataset.StatusActive),
	).Order(ent.Asc(workspacedataset.FieldName)).All(ctx)
	if err != nil {
		return "", nil, err
	}
	return image, &proposalWorkspace{
		nodeID: node.PublicID.String(), nodeLabel: node.Label, path: *access.WorkspacePath,
		datasets: proposalWorkspaceDatasets(datasetRecords),
	}, nil
}

func applyStoredProposalPins(resolved *proposalResolved, environmentSnapshot, projectSnapshot map[string]any) error {
	resolved.cwd = snapshotString(environmentSnapshot, "working_directory")
	resolved.sshHost = snapshotString(environmentSnapshot, "ssh_host")
	resolved.sshUser = snapshotString(environmentSnapshot, "ssh_user")
	resolved.sshNodeID = snapshotString(environmentSnapshot, "ssh_node_id")
	resolved.sshNodeLabel = snapshotString(environmentSnapshot, "ssh_node_label")
	if deps, ok := environmentSnapshot["install_dependencies"].(map[string]any); ok {
		resolved.installDependencies = true
		resolved.requirementsFile = snapshotString(deps, "requirements_file")
	}
	if err := filterProposalBindings(resolved, snapshotString(projectSnapshot, "dataset")); err != nil {
		return ErrProposalChanged
	}
	return nil
}

func proposalReservation(projectRecord *ent.Project, profileRecord *ent.ResourceProfile, runtimeSeconds int) (int64, error) {
	if runtimeSeconds <= 0 || runtimeSeconds > projectRecord.MaxRuntimeSeconds {
		return 0, ErrProposalChanged
	}
	if unmeteredBackend(profileRecord.Backend) {
		return 0, nil
	}
	billable, err := billableRuntimeSeconds(runtimeSeconds, projectRecord.TimeoutExtensionSeconds, projectRecord.TerminationGraceSeconds)
	if err != nil {
		return 0, err
	}
	return reserveCost(profileRecord.PriceToMilli, profileRecord.GpuNum, billable)
}

func storedProposalEligible(checks []map[string]any) bool {
	if len(checks) == 0 {
		return false
	}
	for _, check := range checks {
		status, _ := check["status"].(string)
		if status != ProposalCheckPass && status != ProposalCheckWarn {
			return false
		}
	}
	return true
}

func repositoryRemote(record *ent.Repository) string {
	if record == nil {
		return ""
	}
	return record.SSHURL
}

func proposalDriftError(err error) error {
	if ent.IsNotFound(err) {
		return ErrProposalChanged
	}
	return err
}
