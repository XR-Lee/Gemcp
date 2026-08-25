package nodeaccess

import (
	"context"
	"crypto/hmac"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/nodeassignment"
	"github.com/XR-Lee/Gemcp/ent/nodeenrollment"
	"github.com/XR-Lee/Gemcp/ent/nodeprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/selfhostednode"
	"github.com/XR-Lee/Gemcp/ent/tenant"
	"github.com/XR-Lee/Gemcp/internal/nodeprotocol"
	"github.com/XR-Lee/Gemcp/internal/secrets"
	"github.com/google/uuid"
)

type Service struct {
	client    *ent.Client
	box       *secrets.Box
	publicURL string
	enabled   bool
	projector EventProjector
	now       func() time.Time
}

type EventProjector interface {
	ProjectNodeEvent(context.Context, *ent.Tx, *ent.SelfHostedNode, nodeprotocol.Event, time.Time) error
	ProjectCommandAcknowledgement(context.Context, *ent.Tx, *ent.SelfHostedNode, *ent.NodeCommand, nodeprotocol.CommandAcknowledgement, time.Time) error
}

type ServiceOption func(*Service)

func WithEventProjector(projector EventProjector) ServiceOption {
	return func(service *Service) { service.projector = projector }
}

func NewService(client *ent.Client, box *secrets.Box, publicURL string, enabled bool, options ...ServiceOption) *Service {
	service := &Service{client: client, box: box, publicURL: canonicalOrigin(publicURL), enabled: enabled, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) IssueEnrollment(ctx context.Context, tenantID int, actorID string, input EnrollmentIssueInput) (EnrollmentIssueResult, error) {
	var result EnrollmentIssueResult
	if err := s.ready(); err != nil {
		return result, err
	}
	if s.publicURL == "" {
		return result, ErrPublicURL
	}
	label := strings.TrimSpace(input.Label)
	if label == "" || len(label) > 120 {
		return result, invalid("label is required and must not exceed 120 characters")
	}
	minutes := defaultSetupMinutes
	if input.SetupExpiresInMinutes != nil {
		minutes = *input.SetupExpiresInMinutes
	}
	if minutes < minSetupMinutes || minutes > maxSetupMinutes {
		return result, invalid("setup_expires_in_minutes must be between 5 and 1440")
	}
	now := s.now().UTC()
	expiresAt := now.Add(time.Duration(minutes) * time.Minute)
	rawCode, _, err := secrets.RandomToken("gne", 32)
	if err != nil {
		return result, fmt.Errorf("generate node enrollment code: %w", err)
	}
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin node enrollment: %w", err)
	}
	defer tx.Rollback()
	tenantRecord, err := tx.Tenant.Query().Where(tenant.IDEQ(tenantID), tenant.StatusEQ("active")).Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, fmt.Errorf("load enrollment tenant: %w", err)
	}
	active, err := tx.NodeEnrollment.Query().Where(
		nodeenrollment.TenantIDEQ(tenantID),
		nodeenrollment.StatusIn(nodeenrollment.StatusPending, nodeenrollment.StatusClaimed, nodeenrollment.StatusApproved),
		nodeenrollment.ExpiresAtGT(now),
	).Count(ctx)
	if err != nil {
		return result, fmt.Errorf("count node enrollments: %w", err)
	}
	if active >= maxActiveEnrollments {
		return result, ErrEnrollmentLimit
	}
	record, err := tx.NodeEnrollment.Create().
		SetTenantID(tenantID).
		SetLabel(label).
		SetCodeHash(s.box.Digest("node-enrollment", rawCode)).
		SetExpiresAt(expiresAt).
		Save(ctx)
	if err != nil {
		return result, fmt.Errorf("create node enrollment: %w", err)
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).
		SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("node_enrollment.issued").SetTargetType("node_enrollment").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"label": label, "expires_at": expiresAt.Format(time.RFC3339)}).
		Save(ctx); err != nil {
		return result, fmt.Errorf("write node enrollment audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit node enrollment: %w", err)
	}
	_ = tenantRecord
	fragment := url.Values{"code": []string{rawCode}}.Encode()
	result = EnrollmentIssueResult{
		Enrollment: makeEnrollmentView(record, nil, now),
		SetupURL:   s.publicURL + "/node/setup#" + fragment,
		ClaimURL:   s.publicURL + "/api/v1/node-enrollments/claim",
	}
	return result, nil
}

func (s *Service) ClaimEnrollment(ctx context.Context, input nodeprotocol.EnrollmentClaimRequest) (nodeprotocol.EnrollmentClaimResponse, error) {
	var result nodeprotocol.EnrollmentClaimResponse
	if err := s.ready(); err != nil {
		return result, err
	}
	code := strings.TrimSpace(input.Code)
	if len(code) < 32 || len(code) > 160 || !strings.HasPrefix(code, "gne_") {
		return result, ErrEnrollmentInvalid
	}
	if err := validateInventory(input.Inventory); err != nil {
		return result, err
	}
	now := s.now().UTC()
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin node claim: %w", err)
	}
	defer tx.Rollback()
	record, err := tx.NodeEnrollment.Query().Where(
		nodeenrollment.CodeHashEQ(s.box.Digest("node-enrollment", code)),
	).WithTenant().WithNode().Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrEnrollmentInvalid
	}
	if err != nil {
		return result, fmt.Errorf("find node enrollment: %w", err)
	}
	tenantRecord, err := record.Edges.TenantOrErr()
	if err != nil || tenantRecord.Status != "active" || !record.ExpiresAt.After(now) || record.Status == nodeenrollment.StatusRevoked || record.Status == nodeenrollment.StatusCompleted {
		return result, ErrEnrollmentInvalid
	}
	rawToken, prefix := deriveNodeToken(s.box, code, input.Inventory.InstallationID)
	pairingCode := derivePairingCode(s.box, code, input.Inventory.InstallationID)
	report, err := jsonMap(input.Inventory)
	if err != nil {
		return result, err
	}
	capabilities := map[string]any{
		"cpu_count":    input.Inventory.CPUCount,
		"memory_bytes": input.Inventory.MemoryBytes,
		"gpus":         report["gpus"],
	}
	storage, err := jsonMap(input.Inventory.Storage)
	if err != nil {
		return result, err
	}
	var nodeRecord *ent.SelfHostedNode
	switch record.Status {
	case nodeenrollment.StatusPending:
		existing, err := tx.SelfHostedNode.Query().Where(
			selfhostednode.TenantIDEQ(record.TenantID),
			selfhostednode.InstallationIDEQ(input.Inventory.InstallationID),
		).Exist(ctx)
		if err != nil {
			return result, fmt.Errorf("check node installation identity: %w", err)
		}
		if existing {
			return result, ErrConflict
		}
		nodeRecord, err = tx.SelfHostedNode.Create().
			SetTenantID(record.TenantID).
			SetLabel(record.Label).
			SetTokenPrefix(prefix).
			SetTokenHash(s.box.Digest("node-token", rawToken)).
			SetInstallationID(input.Inventory.InstallationID).
			SetMachineFingerprint(input.Inventory.MachineFingerprint).
			SetHostname(input.Inventory.Hostname).
			SetOperatingSystem(input.Inventory.OperatingSystem).
			SetArchitecture(input.Inventory.Architecture).
			SetAgentVersion(input.Inventory.AgentVersion).
			SetProtocolVersion(input.Inventory.ProtocolVersion).
			SetCapabilities(capabilities).
			SetStorage(storage).
			Save(ctx)
		if err != nil {
			return result, fmt.Errorf("create pending node: %w", err)
		}
		record, err = record.Update().
			SetStatus(nodeenrollment.StatusClaimed).
			SetPairingCode(pairingCode).
			SetInstallationID(input.Inventory.InstallationID).
			SetMachineFingerprint(input.Inventory.MachineFingerprint).
			SetReport(report).
			SetClaimedAt(now).
			SetNode(nodeRecord).
			Save(ctx)
		if err != nil {
			return result, fmt.Errorf("mark node enrollment claimed: %w", err)
		}
		if _, err := tx.AuditEvent.Create().
			SetTenantID(record.TenantID).
			SetActorType("node_enrollment").SetActorID(record.PublicID.String()).
			SetAction("node_enrollment.claimed").SetTargetType("self_hosted_node").SetTargetID(nodeRecord.PublicID.String()).
			SetMetadata(map[string]any{"label": record.Label, "token_prefix": prefix, "pairing_code": pairingCode}).
			Save(ctx); err != nil {
			return result, fmt.Errorf("write node claim audit: %w", err)
		}
	case nodeenrollment.StatusClaimed, nodeenrollment.StatusApproved:
		nodeRecord, err = record.Edges.NodeOrErr()
		if err != nil || record.InstallationID != input.Inventory.InstallationID || record.MachineFingerprint != input.Inventory.MachineFingerprint || record.PairingCode != pairingCode || !hmac.Equal(nodeRecord.TokenHash, s.box.Digest("node-token", rawToken)) {
			return result, ErrEnrollmentInvalid
		}
	default:
		return result, ErrEnrollmentInvalid
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit node claim: %w", err)
	}
	result = nodeprotocol.EnrollmentClaimResponse{
		EnrollmentID: record.PublicID.String(), NodeID: nodeRecord.PublicID.String(), NodeToken: rawToken,
		TokenPrefix: prefix, PairingCode: pairingCode, Status: string(nodeRecord.Status),
		SyncURL: s.publicURL + "/api/v1/nodes/sync", SetupExpiresAt: record.ExpiresAt,
	}
	return result, nil
}

func (s *Service) ApproveEnrollment(ctx context.Context, tenantID int, actorID, enrollmentPublicID string, input ApprovalInput) (NodeView, error) {
	var result NodeView
	if err := s.ready(); err != nil {
		return result, err
	}
	enrollmentID, err := uuid.Parse(strings.TrimSpace(enrollmentPublicID))
	if err != nil {
		return result, ErrNotFound
	}
	pairingCode := strings.ToUpper(strings.TrimSpace(input.PairingCode))
	projectIDs, err := uniqueUUIDs(input.ProjectIDs)
	if err != nil || len(projectIDs) == 0 {
		return result, invalid("project_ids must contain at least one unique Project ID")
	}
	now := s.now().UTC()
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin node approval: %w", err)
	}
	defer tx.Rollback()
	record, err := tx.NodeEnrollment.Query().Where(
		nodeenrollment.PublicIDEQ(enrollmentID), nodeenrollment.TenantIDEQ(tenantID),
	).WithNode().Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, fmt.Errorf("find node enrollment for approval: %w", err)
	}
	if !record.ExpiresAt.After(now) || record.Status == nodeenrollment.StatusPending || record.Status == nodeenrollment.StatusRevoked {
		return result, ErrEnrollmentState
	}
	if record.PairingCode != pairingCode {
		return result, invalid("pairing_code does not match the claimed node")
	}
	nodeRecord, err := record.Edges.NodeOrErr()
	if err != nil {
		return result, ErrEnrollmentState
	}
	projects, err := tx.Project.Query().Where(
		project.PublicIDIn(projectIDs...), project.TenantIDEQ(tenantID), project.StatusNEQ(project.StatusArchived),
	).All(ctx)
	if err != nil {
		return result, fmt.Errorf("load node Projects: %w", err)
	}
	if len(projects) != len(projectIDs) {
		return result, invalid("one or more project_ids are not active Projects in this organization")
	}
	for _, projectRecord := range projects {
		access, err := tx.NodeProjectAccess.Query().Where(
			nodeprojectaccess.NodeIDEQ(nodeRecord.ID), nodeprojectaccess.ProjectIDEQ(projectRecord.ID),
		).Only(ctx)
		switch {
		case ent.IsNotFound(err):
			if _, err := tx.NodeProjectAccess.Create().SetTenantID(tenantID).SetNodeID(nodeRecord.ID).SetProjectID(projectRecord.ID).Save(ctx); err != nil {
				return result, fmt.Errorf("authorize node Project: %w", err)
			}
		case err != nil:
			return result, fmt.Errorf("load node Project authorization: %w", err)
		case access.Status != nodeprojectaccess.StatusActive:
			if _, err := access.Update().SetStatus(nodeprojectaccess.StatusActive).Save(ctx); err != nil {
				return result, fmt.Errorf("reactivate node Project authorization: %w", err)
			}
		}
	}
	if record.Status == nodeenrollment.StatusClaimed {
		record, err = record.Update().SetStatus(nodeenrollment.StatusApproved).SetApprovedAt(now).Save(ctx)
		if err != nil {
			return result, fmt.Errorf("approve node enrollment: %w", err)
		}
	}
	if nodeRecord.Status == selfhostednode.StatusPendingVerification || nodeRecord.Status == selfhostednode.StatusVerificationRequired {
		nodeRecord, err = nodeRecord.Update().SetStatus(selfhostednode.StatusActive).SetApprovedAt(now).Save(ctx)
		if err != nil {
			return result, fmt.Errorf("activate node: %w", err)
		}
	}
	if _, err := tx.AuditEvent.Create().
		SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("node_enrollment.approved").SetTargetType("self_hosted_node").SetTargetID(nodeRecord.PublicID.String()).
		SetMetadata(map[string]any{"enrollment_id": record.PublicID.String(), "project_ids": input.ProjectIDs}).
		Save(ctx); err != nil {
		return result, fmt.Errorf("write node approval audit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit node approval: %w", err)
	}
	return s.nodeView(ctx, nodeRecord.ID)
}

func (s *Service) List(ctx context.Context, tenantID int) (ListResult, error) {
	var result ListResult
	if err := s.ready(); err != nil {
		return result, err
	}
	now := s.now().UTC()
	nodes, err := s.client.SelfHostedNode.Query().Where(selfhostednode.TenantIDEQ(tenantID)).
		WithProjectAccess(func(query *ent.NodeProjectAccessQuery) { query.WithProject() }).
		Order(ent.Asc(selfhostednode.FieldLabel), ent.Asc(selfhostednode.FieldID)).Limit(maxListedNodes + 1).All(ctx)
	if err != nil {
		return result, fmt.Errorf("list nodes: %w", err)
	}
	if len(nodes) > maxListedNodes {
		result.Truncated = true
		nodes = nodes[:maxListedNodes]
	}
	result.Nodes = make([]NodeView, 0, len(nodes))
	for _, nodeRecord := range nodes {
		result.Nodes = append(result.Nodes, makeNodeView(nodeRecord))
	}
	enrollments, err := s.client.NodeEnrollment.Query().Where(nodeenrollment.TenantIDEQ(tenantID)).WithNode().
		Order(ent.Desc(nodeenrollment.FieldCreatedAt)).Limit(maxListedNodes).All(ctx)
	if err != nil {
		return result, fmt.Errorf("list node enrollments: %w", err)
	}
	result.Enrollments = make([]EnrollmentView, 0, len(enrollments))
	for _, enrollment := range enrollments {
		nodeRecord, _ := enrollment.Edges.NodeOrErr()
		result.Enrollments = append(result.Enrollments, makeEnrollmentView(enrollment, nodeRecord, now))
	}
	assignments, err := s.client.NodeAssignment.Query().Where(nodeassignment.TenantIDEQ(tenantID)).
		WithNode().WithProject().WithExperiment().WithAttempt().
		Order(ent.Desc(nodeassignment.FieldCreatedAt)).Limit(maxListedNodes).All(ctx)
	if err != nil {
		return result, fmt.Errorf("list Node Assignments: %w", err)
	}
	result.Assignments = make([]AssignmentView, 0, len(assignments))
	for _, assignment := range assignments {
		nodeRecord, nodeErr := assignment.Edges.NodeOrErr()
		projectRecord, projectErr := assignment.Edges.ProjectOrErr()
		experimentRecord, experimentErr := assignment.Edges.ExperimentOrErr()
		attemptRecord, attemptErr := assignment.Edges.AttemptOrErr()
		if nodeErr != nil || projectErr != nil || experimentErr != nil || attemptErr != nil {
			return result, fmt.Errorf("Node Assignment has incomplete ownership edges")
		}
		result.Assignments = append(result.Assignments, AssignmentView{
			ID: assignment.PublicID.String(), NodeID: nodeRecord.PublicID.String(), NodeLabel: nodeRecord.Label,
			ProjectID: projectRecord.PublicID.String(), ExperimentID: experimentRecord.PublicID.String(),
			AttemptID: attemptRecord.PublicID.String(), AttemptNumber: attemptRecord.Number,
			State: string(assignment.State), OutputRef: assignment.OutputRef, StartedAt: assignment.StartedAt,
			LastHeartbeat: assignment.LastHeartbeatAt, FinishedAt: assignment.FinishedAt, ExitCode: assignment.ExitCode,
			FailureCode: assignment.FailureCode, CreatedAt: assignment.CreatedAt, UpdatedAt: assignment.UpdatedAt,
		})
	}
	return result, nil
}

func (s *Service) RevokeEnrollment(ctx context.Context, tenantID int, actorID, enrollmentPublicID string) (EnrollmentView, error) {
	var result EnrollmentView
	if err := s.ready(); err != nil {
		return result, err
	}
	id, err := uuid.Parse(strings.TrimSpace(enrollmentPublicID))
	if err != nil {
		return result, ErrNotFound
	}
	now := s.now().UTC()
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return result, fmt.Errorf("begin node enrollment revocation: %w", err)
	}
	defer tx.Rollback()
	record, err := tx.NodeEnrollment.Query().Where(nodeenrollment.PublicIDEQ(id), nodeenrollment.TenantIDEQ(tenantID)).WithNode().Only(ctx)
	if ent.IsNotFound(err) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, fmt.Errorf("find node enrollment: %w", err)
	}
	nodeRecord, _ := record.Edges.NodeOrErr()
	if record.Status != nodeenrollment.StatusRevoked {
		record, err = record.Update().SetStatus(nodeenrollment.StatusRevoked).Save(ctx)
		if err != nil {
			return result, fmt.Errorf("revoke node enrollment: %w", err)
		}
		if nodeRecord != nil && nodeRecord.Status != selfhostednode.StatusRevoked {
			nodeRecord, err = nodeRecord.Update().SetStatus(selfhostednode.StatusRevoked).SetRevokedAt(now).Save(ctx)
			if err != nil {
				return result, fmt.Errorf("revoke node credential: %w", err)
			}
		}
		if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
			SetAction("node_enrollment.revoked").SetTargetType("node_enrollment").SetTargetID(record.PublicID.String()).
			SetMetadata(map[string]any{"node_id": nodePublicID(nodeRecord)}).Save(ctx); err != nil {
			return result, fmt.Errorf("write node revocation audit: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("commit node enrollment revocation: %w", err)
	}
	return makeEnrollmentView(record, nodeRecord, now), nil
}

func (s *Service) ready() error {
	if s == nil || s.client == nil || s.box == nil {
		return fmt.Errorf("node access service is not initialized")
	}
	if !s.enabled {
		return ErrDisabled
	}
	return nil
}

func (s *Service) nodeView(ctx context.Context, id int) (NodeView, error) {
	record, err := s.client.SelfHostedNode.Query().Where(selfhostednode.IDEQ(id)).
		WithProjectAccess(func(query *ent.NodeProjectAccessQuery) { query.WithProject() }).Only(ctx)
	if err != nil {
		return NodeView{}, err
	}
	return makeNodeView(record), nil
}

func makeNodeView(record *ent.SelfHostedNode) NodeView {
	projects := make([]string, 0, len(record.Edges.ProjectAccess))
	for _, access := range record.Edges.ProjectAccess {
		if access.Status != nodeprojectaccess.StatusActive || access.Edges.Project == nil {
			continue
		}
		projects = append(projects, access.Edges.Project.PublicID.String())
	}
	sort.Strings(projects)
	return NodeView{
		ID: record.PublicID.String(), Label: record.Label, TokenPrefix: record.TokenPrefix,
		Status: string(record.Status), ObservedState: string(record.ObservedState), InstallationID: record.InstallationID,
		MachineFingerprint: record.MachineFingerprint, Hostname: record.Hostname, OperatingSystem: record.OperatingSystem,
		Architecture: record.Architecture, AgentVersion: record.AgentVersion, ProtocolVersion: record.ProtocolVersion,
		Capabilities: cloneMap(record.Capabilities), Storage: cloneMap(record.Storage), ProjectIDs: projects,
		LastSeenAt: record.LastSeenAt, ApprovedAt: record.ApprovedAt, RevokedAt: record.RevokedAt,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func makeEnrollmentView(record *ent.NodeEnrollment, nodeRecord *ent.SelfHostedNode, now time.Time) EnrollmentView {
	status := string(record.Status)
	if record.Status != nodeenrollment.StatusCompleted && record.Status != nodeenrollment.StatusRevoked && !record.ExpiresAt.After(now) {
		status = "expired"
	}
	return EnrollmentView{
		ID: record.PublicID.String(), Label: record.Label, Status: status, ExpiresAt: record.ExpiresAt,
		PairingCode: record.PairingCode, InstallationID: record.InstallationID, MachineFingerprint: record.MachineFingerprint,
		Report: cloneMap(record.Report), NodeID: nodePublicID(nodeRecord), ClaimedAt: record.ClaimedAt,
		ApprovedAt: record.ApprovedAt, CompletedAt: record.CompletedAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func nodePublicID(record *ent.SelfHostedNode) string {
	if record == nil {
		return ""
	}
	return record.PublicID.String()
}

func validateInventory(value nodeprotocol.Inventory) error {
	if _, err := uuid.Parse(strings.TrimSpace(value.InstallationID)); err != nil {
		return invalid("inventory.installation_id must be a UUID")
	}
	if len(value.MachineFingerprint) != 64 {
		return invalid("inventory.machine_fingerprint must be a 64-character digest")
	}
	if strings.TrimSpace(value.Hostname) == "" || len(value.Hostname) > 255 {
		return invalid("inventory.hostname is required and must not exceed 255 characters")
	}
	if value.OperatingSystem != "linux" || value.Architecture != "amd64" {
		return invalid("the initial node release requires linux/amd64")
	}
	if value.ProtocolVersion != nodeprotocol.Version {
		return ErrProtocol
	}
	if strings.TrimSpace(value.AgentVersion) == "" || len(value.AgentVersion) > 64 {
		return invalid("inventory.agent_version is required and must not exceed 64 characters")
	}
	if value.CPUCount <= 0 || value.MemoryBytes <= 0 {
		return invalid("inventory CPU and memory values must be positive")
	}
	if len(value.GPUs) != 1 || !strings.HasPrefix(value.GPUs[0].UUID, "GPU-") || len(value.GPUs[0].UUID) > 120 ||
		strings.TrimSpace(value.GPUs[0].Name) == "" || len(value.GPUs[0].Name) > 120 || value.GPUs[0].MemoryBytes <= 0 {
		return invalid("the initial node release requires exactly one identified NVIDIA GPU")
	}
	if !path.IsAbs(value.Storage.Root) || value.Storage.TotalBytes <= 0 || value.Storage.AvailableBytes < 0 || value.Storage.ManagedBytes < 0 {
		return invalid("inventory.storage must contain an absolute root and valid capacity values")
	}
	return nil
}

func deriveNodeToken(box *secrets.Box, code, installationID string) (string, string) {
	material := box.Digest("node-enrollment-node-token", code+"\x00"+installationID)
	prefix := "gmn_" + base64.RawURLEncoding.EncodeToString(material[:5])
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(material), prefix
}

func derivePairingCode(box *secrets.Box, code, installationID string) string {
	material := box.Digest("node-enrollment-pairing", code+"\x00"+installationID)
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(material[:5])
	return encoded[:4] + "-" + encoded[4:8]
}

func uniqueUUIDs(raw []string) ([]uuid.UUID, error) {
	seen := map[uuid.UUID]bool{}
	result := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		parsed, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil || seen[parsed] {
			return nil, invalid("project_ids must contain unique UUIDs")
		}
		seen[parsed] = true
		result = append(result, parsed)
	}
	return result, nil
}

func jsonMap(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode node report: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("normalize node report: %w", err)
	}
	return result, nil
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func canonicalOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Trim(parsed.Path, "/") != "" {
		return ""
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		host := parsed.Hostname()
		if !strings.EqualFold(host, "localhost") {
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return ""
			}
		}
	default:
		return ""
	}
	return strings.TrimRight(parsed.String(), "/")
}
