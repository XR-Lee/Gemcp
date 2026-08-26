package experiment

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/agenttoken"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	entexperiment "github.com/XR-Lee/Gemcp/ent/experiment"
	"github.com/XR-Lee/Gemcp/ent/experimentproposal"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
	"github.com/google/uuid"
)

var activityPhases = map[string]struct{}{
	"inspecting_repository": {}, "selecting_workload": {}, "preparing_proposal": {}, "awaiting_confirmation": {},
	"submitting": {}, "monitoring": {}, "reviewing_results": {}, "blocked": {}, "idle": {},
}

type ReportActivityInput struct {
	Phase            string `json:"phase" jsonschema:"controlled activity phase: inspecting_repository, selecting_workload, preparing_proposal, awaiting_confirmation, submitting, monitoring, reviewing_results, blocked, or idle"`
	RepositoryRemote string `json:"repository_remote,omitempty" jsonschema:"registered Git remote currently being inspected"`
	Ref              string `json:"ref,omitempty" jsonschema:"branch, tag, or commit currently being inspected"`
	ExperimentID     string `json:"experiment_id,omitempty" jsonschema:"current Gemcp Experiment ID when monitoring or reviewing a submitted run"`
}

type AgentActivityView struct {
	ID               string    `json:"id"`
	AgentLabel       string    `json:"agent_label"`
	AgentTokenPrefix string    `json:"agent_token_prefix"`
	Phase            string    `json:"phase"`
	RepositoryRemote string    `json:"repository_remote,omitempty"`
	Ref              string    `json:"ref,omitempty"`
	ProposalID       string    `json:"proposal_id,omitempty"`
	ExperimentID     string    `json:"experiment_id,omitempty"`
	At               time.Time `json:"at"`
}

type ReportActivityResult struct {
	Activity AgentActivityView `json:"activity"`
}

type ProposalActivityView struct {
	ID                  string          `json:"id"`
	Status              string          `json:"status"`
	Eligible            bool            `json:"eligible"`
	AgentLabel          string          `json:"agent_label"`
	AgentTokenPrefix    string          `json:"agent_token_prefix"`
	RepositoryName      string          `json:"repository_name"`
	RequestedRef        string          `json:"requested_ref"`
	CommitSHA           string          `json:"commit_sha"`
	DisplayCommand      string          `json:"display_command"`
	Backend             string          `json:"backend"`
	EnvironmentName     string          `json:"environment_name"`
	Image               string          `json:"image"`
	ResourceProfileName string          `json:"resource_profile_name"`
	GPUModels           []string        `json:"gpu_models"`
	GPUNum              int             `json:"gpu_num"`
	RuntimePreset       string          `json:"runtime_preset"`
	MaxRuntimeSeconds   int             `json:"max_runtime_seconds"`
	ReservedCostMilli   int64           `json:"reserved_cost_milli"`
	Checks              []ProposalCheck `json:"checks"`
	ConfirmationDigest  string          `json:"confirmation_digest"`
	ExperimentID        string          `json:"experiment_id,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	ExpiresAt           time.Time       `json:"expires_at"`
}

type OperationsFeed struct {
	Activities  []AgentActivityView    `json:"activities"`
	Proposals   []ProposalActivityView `json:"proposals"`
	GeneratedAt time.Time              `json:"generated_at"`
}

func (s *Service) ReportActivity(ctx context.Context, principal agentauth.Principal, input ReportActivityInput) (ReportActivityResult, error) {
	if !principal.HasScope("submit") {
		return ReportActivityResult{}, ErrForbidden
	}
	phase := strings.TrimSpace(input.Phase)
	if _, ok := activityPhases[phase]; !ok {
		return ReportActivityResult{}, &ValidationError{Message: "unsupported Agent activity phase"}
	}
	remote, err := boundedActivityValue(input.RepositoryRemote, 512)
	if err != nil {
		return ReportActivityResult{}, &ValidationError{Message: "repository_remote is invalid"}
	}
	ref, err := boundedActivityValue(input.Ref, 255)
	if err != nil {
		return ReportActivityResult{}, &ValidationError{Message: "ref is invalid"}
	}
	experimentID := strings.TrimSpace(input.ExperimentID)
	if (phase == "monitoring" || phase == "reviewing_results") && experimentID == "" {
		return ReportActivityResult{}, &ValidationError{Message: "experiment_id is required while monitoring or reviewing results"}
	}
	if experimentID != "" {
		publicID, parseErr := uuid.Parse(experimentID)
		if parseErr != nil {
			return ReportActivityResult{}, &ValidationError{Message: "experiment_id is invalid"}
		}
		exists, queryErr := s.client.Experiment.Query().Where(
			entexperiment.PublicIDEQ(publicID), entexperiment.ProjectIDEQ(principal.ProjectID), entexperiment.AgentTokenIDEQ(principal.TokenID),
		).Exist(ctx)
		if queryErr != nil {
			return ReportActivityResult{}, queryErr
		}
		if !exists {
			return ReportActivityResult{}, ErrNotFound
		}
	}
	return s.recordActivity(ctx, principal, phase, remote, ref, "", experimentID)
}

func (s *Service) recordActivity(ctx context.Context, principal agentauth.Principal, phase, remote, ref, proposalID, experimentID string) (ReportActivityResult, error) {
	metadata := map[string]any{"project_id": principal.ProjectPublicID, "phase": phase}
	if remote != "" {
		metadata["repository_remote"] = remote
	}
	if ref != "" {
		metadata["ref"] = ref
	}
	if proposalID != "" {
		metadata["proposal_id"] = proposalID
	}
	if experimentID != "" {
		metadata["experiment_id"] = experimentID
	}
	record, err := s.client.AuditEvent.Create().SetTenantID(principal.TenantID).SetActorType(auditevent.ActorTypeAgentToken).
		SetActorID(principal.TokenPublicID).SetAction("agent.activity").SetTargetType("project").SetTargetID(principal.ProjectPublicID).
		SetMetadata(metadata).Save(ctx)
	if err != nil {
		return ReportActivityResult{}, err
	}
	return ReportActivityResult{Activity: AgentActivityView{
		ID: record.PublicID.String(), AgentLabel: principal.TokenLabel, AgentTokenPrefix: principal.TokenPrefix, Phase: phase,
		RepositoryRemote: remote, Ref: ref, ProposalID: proposalID, ExperimentID: experimentID, At: record.CreatedAt,
	}}, nil
}

func (s *Service) OwnerOperations(ctx context.Context, tenantID int, projectID string, limit int) (OperationsFeed, error) {
	var result OperationsFeed
	publicID, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return result, ErrNotFound
	}
	projectRecord, err := s.client.Project.Query().Where(project.PublicIDEQ(publicID), project.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return result, &ValidationError{Message: "limit must be between 1 and 100"}
	}
	events, err := s.client.AuditEvent.Query().Where(
		auditevent.TenantIDEQ(tenantID), auditevent.ActionEQ("agent.activity"),
		auditevent.TargetTypeEQ("project"), auditevent.TargetIDEQ(projectRecord.PublicID.String()),
	).Order(ent.Desc(auditevent.FieldCreatedAt), ent.Desc(auditevent.FieldID)).Limit(limit).All(ctx)
	if err != nil {
		return result, err
	}
	tokens, err := s.client.AgentToken.Query().Where(agenttoken.ProjectIDEQ(projectRecord.ID)).All(ctx)
	if err != nil {
		return result, err
	}
	byPublicID := map[string]*ent.AgentToken{}
	for _, token := range tokens {
		byPublicID[token.PublicID.String()] = token
	}
	result.Activities = make([]AgentActivityView, 0, len(events))
	for _, event := range events {
		token := byPublicID[event.ActorID]
		activity := AgentActivityView{
			ID: event.PublicID.String(), Phase: metadataString(event.Metadata, "phase"), RepositoryRemote: metadataString(event.Metadata, "repository_remote"),
			Ref: metadataString(event.Metadata, "ref"), ProposalID: metadataString(event.Metadata, "proposal_id"),
			ExperimentID: metadataString(event.Metadata, "experiment_id"), At: event.CreatedAt,
		}
		if token != nil {
			activity.AgentLabel, activity.AgentTokenPrefix = token.Label, token.Prefix
		}
		result.Activities = append(result.Activities, activity)
	}
	proposals, err := s.client.ExperimentProposal.Query().Where(experimentproposal.ProjectIDEQ(projectRecord.ID)).
		WithAgentToken().WithExperiment().Order(ent.Desc(experimentproposal.FieldCreatedAt)).Limit(limit).All(ctx)
	if err != nil {
		return result, err
	}
	result.Proposals = make([]ProposalActivityView, 0, len(proposals))
	now := s.now().UTC()
	for _, proposal := range proposals {
		status := string(proposal.Status)
		if status == "prepared" && !now.Before(proposal.ExpiresAt) {
			status = "expired"
		}
		checks := proposalChecksFromMaps(proposal.Checks)
		view := ProposalActivityView{
			ID: proposal.PublicID.String(), Status: status, Eligible: proposalChecksEligible(checks), RequestedRef: proposal.RequestedRef,
			CommitSHA: proposal.CommitSha, DisplayCommand: proposal.DisplayCommand, Backend: snapshotString(proposal.ResourceSnapshot, "backend"),
			RepositoryName: snapshotString(proposal.RepositorySnapshot, "name"), EnvironmentName: snapshotString(proposal.EnvironmentSnapshot, "name"),
			Image: snapshotString(proposal.EnvironmentSnapshot, "image_uuid"), ResourceProfileName: snapshotString(proposal.ResourceSnapshot, "name"),
			GPUModels: snapshotStrings(proposal.ResourceSnapshot, "gpu_names"), GPUNum: snapshotInt(proposal.ResourceSnapshot, "gpu_num"),
			RuntimePreset: proposal.RuntimePreset, MaxRuntimeSeconds: proposal.MaxRuntimeSeconds,
			ReservedCostMilli: proposal.ReservedCostMilli, Checks: checks, ConfirmationDigest: proposal.ConfirmationDigest,
			CreatedAt: proposal.CreatedAt, UpdatedAt: proposal.UpdatedAt, ExpiresAt: proposal.ExpiresAt,
		}
		if token, edgeErr := proposal.Edges.AgentTokenOrErr(); edgeErr == nil {
			view.AgentLabel, view.AgentTokenPrefix = token.Label, token.Prefix
		}
		if experimentRecord, edgeErr := proposal.Edges.ExperimentOrErr(); edgeErr == nil {
			view.ExperimentID = experimentRecord.PublicID.String()
		}
		result.Proposals = append(result.Proposals, view)
	}
	result.GeneratedAt = now
	return result, nil
}

func proposalChecksFromMaps(values []map[string]any) []ProposalCheck {
	result := make([]ProposalCheck, 0, len(values))
	for _, value := range values {
		result = append(result, ProposalCheck{
			ID: metadataString(value, "id"), Status: metadataString(value, "status"),
			Summary: metadataString(value, "summary"), Detail: metadataString(value, "detail"),
		})
	}
	return result
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func boundedActivityValue(value string, maximum int) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		return "", fmt.Errorf("value exceeds bound")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("value contains a control character")
		}
	}
	return value, nil
}
