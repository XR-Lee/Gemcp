package sshcloud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/cloudsshassignment"
	"github.com/XR-Lee/Gemcp/ent/cloudsshnode"
	"github.com/XR-Lee/Gemcp/ent/cloudsshprojectaccess"
	"github.com/XR-Lee/Gemcp/ent/environment"
	"github.com/XR-Lee/Gemcp/ent/project"
	"github.com/XR-Lee/Gemcp/ent/resourceprofile"
	"github.com/google/uuid"
)

var runtimeNameCharacters = regexp.MustCompile(`[^a-z0-9]+`)

func (s *Service) List(ctx context.Context, tenantID int) (ListResult, error) {
	result := ListResult{Experimental: true, Warning: WarningOwner, Enabled: s.Enabled(), Nodes: []NodeView{}, Assignments: []AssignmentView{}}
	if !s.Enabled() {
		return result, nil
	}
	nodes, err := s.client.CloudSSHNode.Query().Where(cloudsshnode.TenantIDEQ(tenantID)).
		WithProjectAccess(func(query *ent.CloudSSHProjectAccessQuery) {
			query.Where(cloudsshprojectaccess.StatusEQ(cloudsshprojectaccess.StatusActive)).WithProject()
		}).
		Order(ent.Asc(cloudsshnode.FieldLabel), ent.Asc(cloudsshnode.FieldID)).All(ctx)
	if err != nil {
		return result, err
	}
	for _, node := range nodes {
		runtimes, err := s.projectRuntimeViews(ctx, node)
		if err != nil {
			return result, err
		}
		result.Nodes = append(result.Nodes, nodeView(node, runtimes))
	}
	assignments, err := s.client.CloudSSHAssignment.Query().Where(cloudsshassignment.TenantIDEQ(tenantID)).
		WithNode().WithExperiment().WithAttempt().
		Order(ent.Desc(cloudsshassignment.FieldCreatedAt), ent.Desc(cloudsshassignment.FieldID)).Limit(200).All(ctx)
	if err != nil {
		return result, err
	}
	for _, assignment := range assignments {
		result.Assignments = append(result.Assignments, assignmentView(assignment))
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, tenantID int, actorID string, input CreateInput) (NodeView, error) {
	if !s.Enabled() {
		return NodeView{}, ErrDisabled
	}
	applyLocalProcessDefaults(&input, s.config.LocalProcessEnabled)
	if err := applySSHTarget(&input); err != nil {
		return NodeView{}, err
	}
	label, err := normalizeLabel(input.Label)
	if err != nil {
		return NodeView{}, err
	}
	target, err := normalizeTarget(input.Host, input.Port, input.User, s.config.LocalProcessEnabled)
	if err != nil {
		return NodeView{}, err
	}
	active, err := s.client.CloudSSHNode.Query().Where(
		cloudsshnode.TenantIDEQ(tenantID), cloudsshnode.StatusNEQ(cloudsshnode.StatusRevoked),
	).Count(ctx)
	if err != nil {
		return NodeView{}, err
	}
	if active >= maxActiveNodes {
		return NodeView{}, ErrNodeLimit
	}
	ciphertext, err := EncryptCredential(s.box, Credential{
		Method: input.AuthMethod, Password: input.Password, PrivateKey: input.PrivateKey, Passphrase: input.Passphrase,
	})
	if err != nil {
		return NodeView{}, err
	}
	actorType, actor := parseActor(actorID)
	create := s.client.CloudSSHNode.Create().
		SetTenantID(tenantID).SetLabel(label).SetSSHHost(target.Host).SetSSHPort(target.Port).SetSSHUser(target.User).
		SetAuthMethod(cloudsshnode.AuthMethod(strings.ToLower(strings.TrimSpace(input.AuthMethod)))).
		SetCredentialCiphertext(ciphertext).SetCreatedActorType(actorType).SetCreatedActorID(actor)
	record, err := create.Save(ctx)
	if err != nil {
		return NodeView{}, err
	}
	if _, err := s.client.AuditEvent.Create().SetTenantID(tenantID).SetActorType(auditActorType(actorType)).SetActorID(actor).
		SetAction("ssh_cloud.node_created").SetTargetType("cloud_ssh_node").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"host": target.Host, "port": target.Port, "user": target.User, "auth_method": record.AuthMethod}).Save(ctx); err != nil {
		return NodeView{}, err
	}
	if projectID := strings.TrimSpace(input.ProjectID); projectID != "" {
		if _, err := s.Authorize(ctx, tenantID, actorID, record.PublicID.String(), AuthorizeInput{
			ProjectIDs: []string{projectID}, Image: HostImage, MakeDefault: true,
		}); err != nil && !errors.Is(err, ErrNotFound) {
			var validation *ValidationError
			if !errors.As(err, &validation) || !strings.Contains(validation.Message, "probe") {
				return NodeView{}, err
			}
		}
	}
	shouldProbe := input.Probe == nil || *input.Probe
	if shouldProbe {
		return s.Probe(ctx, tenantID, actorID, record.PublicID.String())
	}
	s.startBackgroundProbe(tenantID, actorID, record.PublicID.String())
	return nodeView(record, nil), nil
}

func (s *Service) startBackgroundProbe(tenantID int, actorID, nodeID string) {
	if s == nil || s.skipAsyncProbe {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, _ = s.Probe(ctx, tenantID, actorID, nodeID)
	}()
}

func parseActor(actorID string) (string, string) {
	actorID = strings.TrimSpace(actorID)
	if after, ok := strings.CutPrefix(actorID, "agent:"); ok {
		return "agent", strings.TrimSpace(after)
	}
	if after, ok := strings.CutPrefix(actorID, "user:"); ok {
		return "user", strings.TrimSpace(after)
	}
	if actorID == "" {
		return "user", "unknown"
	}
	return "user", actorID
}

func auditActorType(actorType string) auditevent.ActorType {
	if actorType == "agent" {
		return auditevent.ActorTypeAgentToken
	}
	return auditevent.ActorTypeUser
}

func (s *Service) Probe(ctx context.Context, tenantID int, actorID, nodeID string) (NodeView, error) {
	if !s.Enabled() {
		return NodeView{}, ErrDisabled
	}
	id, err := uuid.Parse(strings.TrimSpace(nodeID))
	if err != nil {
		return NodeView{}, ErrNotFound
	}
	record, err := s.client.CloudSSHNode.Query().Where(cloudsshnode.PublicIDEQ(id), cloudsshnode.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return NodeView{}, ErrNotFound
	}
	if err != nil {
		return NodeView{}, err
	}
	if record.Status == cloudsshnode.StatusRevoked {
		return NodeView{}, invalid("revoked Cloud SSH nodes cannot be probed")
	}
	credential, err := DecryptCredential(s.box, record.CredentialCiphertext)
	if err != nil {
		return NodeView{}, err
	}
	target := Target{Host: record.SSHHost, Port: record.SSHPort, User: record.SSHUser}
	logger := newProbeLogger(s, record, []string{credential.Password, credential.Passphrase})
	ctx = withProbeLogger(ctx, logger)
	logger.step(ctx, StepConnect, fmt.Sprintf("Connecting to %s@%s:%d", target.User, target.Host, target.Port))
	conn, fingerprint, err := s.openNode(ctx, target, credential, record.HostKeyFingerprint)
	if err == ErrHostKeyChanged {
		logger.fail(ctx, ErrHostKeyChanged.Error())
		if _, updateErr := record.Update().SetStatus(cloudsshnode.StatusHostKeyChanged).SetInventory(withProbeLog(record.Inventory, logger.snapshot())).Save(ctx); updateErr != nil {
			return NodeView{}, updateErr
		}
		return NodeView{}, ErrHostKeyChanged
	}
	if err != nil {
		logger.fail(ctx, err.Error())
		return NodeView{}, err
	}
	defer conn.Close()
	inventory, err := s.probeConn(ctx, conn)
	if err != nil {
		logger.fail(ctx, err.Error())
		return NodeView{}, err
	}
	names := make([]string, 0, len(inventory.GPUs))
	for _, gpu := range inventory.GPUs {
		names = append(names, gpu.Name)
	}
	summary := strings.TrimSpace(inventory.OS + " " + inventory.Arch)
	if summary == "" {
		summary = "reachable"
	}
	if len(names) > 0 {
		summary += " · " + strings.Join(names, ", ")
	}
	logger.succeed(ctx, "Probe finished · "+summary)
	now := s.now().UTC()
	updated, err := record.Update().SetStatus(cloudsshnode.StatusActive).SetHostKeyFingerprint(fingerprint).
		SetInventory(withProbeLog(inventoryMap(inventory), logger.snapshot())).SetLastProbedAt(now).Save(ctx)
	if err != nil {
		return NodeView{}, err
	}
	actorType, actor := parseActor(actorID)
	if _, err := s.client.AuditEvent.Create().SetTenantID(tenantID).SetActorType(auditActorType(actorType)).SetActorID(actor).
		SetAction("ssh_cloud.node_probed").SetTargetType("cloud_ssh_node").SetTargetID(updated.PublicID.String()).
		SetMetadata(map[string]any{"host_key_fingerprint": fingerprint, "gpu_names": inventoryGPUNames(updated.Inventory)}).Save(ctx); err != nil {
		return NodeView{}, err
	}
	return s.loadedView(ctx, updated.ID)
}

func (s *Service) Authorize(ctx context.Context, tenantID int, actorID, nodeID string, input AuthorizeInput) (NodeView, error) {
	if !s.Enabled() {
		return NodeView{}, ErrDisabled
	}
	id, err := uuid.Parse(strings.TrimSpace(nodeID))
	if err != nil {
		return NodeView{}, ErrNotFound
	}
	image := HostImage
	if len(input.ProjectIDs) == 0 {
		return NodeView{}, invalid("at least one Project must be authorized")
	}
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return NodeView{}, err
	}
	defer tx.Rollback()
	record, err := tx.CloudSSHNode.Query().Where(cloudsshnode.PublicIDEQ(id), cloudsshnode.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return NodeView{}, ErrNotFound
	}
	if err != nil {
		return NodeView{}, err
	}
	if record.Status == cloudsshnode.StatusRevoked {
		return NodeView{}, invalid("revoked Cloud SSH nodes cannot be authorized")
	}
	gpuNames := inventoryGPUNames(record.Inventory)
	if len(gpuNames) == 0 {
		gpuNames = []string{"host"}
	}
	cpuLimit, memoryGB := hardwareLimits(record.Inventory)
	for _, rawID := range input.ProjectIDs {
		projectID, parseErr := uuid.Parse(strings.TrimSpace(rawID))
		if parseErr != nil {
			return NodeView{}, invalid("project_ids must be UUIDs")
		}
		projectRecord, projectErr := tx.Project.Query().Where(project.PublicIDEQ(projectID), project.TenantIDEQ(tenantID), project.StatusNEQ(project.StatusArchived)).Only(ctx)
		if ent.IsNotFound(projectErr) {
			return NodeView{}, ErrProject
		}
		if projectErr != nil {
			return NodeView{}, projectErr
		}
		access, accessErr := tx.CloudSSHProjectAccess.Query().Where(
			cloudsshprojectaccess.NodeIDEQ(record.ID), cloudsshprojectaccess.ProjectIDEQ(projectRecord.ID),
		).Only(ctx)
		if accessErr != nil && !ent.IsNotFound(accessErr) {
			return NodeView{}, accessErr
		}
		if ent.IsNotFound(accessErr) {
			if _, err := tx.CloudSSHProjectAccess.Create().SetTenantID(tenantID).SetNodeID(record.ID).SetProjectID(projectRecord.ID).Save(ctx); err != nil {
				return NodeView{}, err
			}
		} else if access.Status != cloudsshprojectaccess.StatusActive {
			if _, err := access.Update().SetStatus(cloudsshprojectaccess.StatusActive).Save(ctx); err != nil {
				return NodeView{}, err
			}
		}
		if err := s.ensureRuntime(ctx, tx, tenantID, actorID, record, projectRecord, image, gpuNames, cpuLimit, memoryGB, input.MakeDefault); err != nil {
			return NodeView{}, err
		}
	}
	actorType, actor := parseActor(actorID)
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType(auditActorType(actorType)).SetActorID(actor).
		SetAction("ssh_cloud.node_authorized").SetTargetType("cloud_ssh_node").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"project_ids": input.ProjectIDs, "image": image, "make_default": input.MakeDefault}).Save(ctx); err != nil {
		return NodeView{}, err
	}
	if err := tx.Commit(); err != nil {
		return NodeView{}, err
	}
	return s.loadedView(ctx, record.ID)
}

func (s *Service) Rotate(ctx context.Context, tenantID int, actorID, nodeID string, input RotateInput) (NodeView, error) {
	if !s.Enabled() {
		return NodeView{}, ErrDisabled
	}
	id, err := uuid.Parse(strings.TrimSpace(nodeID))
	if err != nil {
		return NodeView{}, ErrNotFound
	}
	ciphertext, err := EncryptCredential(s.box, Credential{
		Method: input.AuthMethod, Password: input.Password, PrivateKey: input.PrivateKey, Passphrase: input.Passphrase,
	})
	if err != nil {
		return NodeView{}, err
	}
	record, err := s.client.CloudSSHNode.Query().Where(cloudsshnode.PublicIDEQ(id), cloudsshnode.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return NodeView{}, ErrNotFound
	}
	if err != nil {
		return NodeView{}, err
	}
	if record.Status == cloudsshnode.StatusRevoked {
		return NodeView{}, invalid("revoked Cloud SSH nodes cannot rotate credentials")
	}
	if _, err := record.Update().SetAuthMethod(cloudsshnode.AuthMethod(strings.ToLower(strings.TrimSpace(input.AuthMethod)))).
		SetCredentialCiphertext(ciphertext).Save(ctx); err != nil {
		return NodeView{}, err
	}
	actorType, actor := parseActor(actorID)
	if _, err := s.client.AuditEvent.Create().SetTenantID(tenantID).SetActorType(auditActorType(actorType)).SetActorID(actor).
		SetAction("ssh_cloud.credential_rotated").SetTargetType("cloud_ssh_node").SetTargetID(record.PublicID.String()).
		SetMetadata(map[string]any{"auth_method": strings.ToLower(strings.TrimSpace(input.AuthMethod))}).Save(ctx); err != nil {
		return NodeView{}, err
	}
	shouldProbe := input.Probe == nil || *input.Probe
	if shouldProbe {
		return s.Probe(ctx, tenantID, actorID, record.PublicID.String())
	}
	s.startBackgroundProbe(tenantID, actorID, record.PublicID.String())
	return s.loadedView(ctx, record.ID)
}

func (s *Service) Revoke(ctx context.Context, tenantID int, actorID, nodeID string) (NodeView, error) {
	if !s.Enabled() {
		return NodeView{}, ErrDisabled
	}
	id, err := uuid.Parse(strings.TrimSpace(nodeID))
	if err != nil {
		return NodeView{}, ErrNotFound
	}
	tx, err := s.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return NodeView{}, err
	}
	defer tx.Rollback()
	record, err := tx.CloudSSHNode.Query().Where(cloudsshnode.PublicIDEQ(id), cloudsshnode.TenantIDEQ(tenantID)).Only(ctx)
	if ent.IsNotFound(err) {
		return NodeView{}, ErrNotFound
	}
	if err != nil {
		return NodeView{}, err
	}
	busy, err := tx.CloudSSHAssignment.Query().Where(
		cloudsshassignment.NodeIDEQ(record.ID),
		cloudsshassignment.StateIn(cloudsshassignment.StateStarting, cloudsshassignment.StateRunning, cloudsshassignment.StateStopping, cloudsshassignment.StateCollecting),
	).Exist(ctx)
	if err != nil {
		return NodeView{}, err
	}
	if busy {
		return NodeView{}, ErrBusy
	}
	now := s.now().UTC()
	if _, err := tx.CloudSSHProjectAccess.Update().Where(cloudsshprojectaccess.NodeIDEQ(record.ID), cloudsshprojectaccess.StatusEQ(cloudsshprojectaccess.StatusActive)).
		SetStatus(cloudsshprojectaccess.StatusRevoked).Save(ctx); err != nil {
		return NodeView{}, err
	}
	updated, err := record.Update().SetStatus(cloudsshnode.StatusRevoked).SetRevokedAt(now).Save(ctx)
	if err != nil {
		return NodeView{}, err
	}
	actorType, actor := parseActor(actorID)
	if _, err := tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType(auditActorType(actorType)).SetActorID(actor).
		SetAction("ssh_cloud.node_revoked").SetTargetType("cloud_ssh_node").SetTargetID(updated.PublicID.String()).Save(ctx); err != nil {
		return NodeView{}, err
	}
	if err := tx.Commit(); err != nil {
		return NodeView{}, err
	}
	return nodeView(updated, nil), nil
}

func (s *Service) ensureRuntime(ctx context.Context, tx *ent.Tx, tenantID int, actorID string, node *ent.CloudSSHNode, projectRecord *ent.Project, image string, gpuNames []string, cpuLimit, memoryGB int, makeDefault bool) error {
	recipe := recipePrefix + node.PublicID.String()
	environmentRecord, err := tx.Environment.Query().Where(environment.ProjectIDEQ(projectRecord.ID), environment.RecipeRefEQ(recipe)).Only(ctx)
	name := runtimeName(node.Label, node.PublicID)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	image = HostImage
	if makeDefault {
		if _, err := tx.Environment.Update().Where(environment.ProjectIDEQ(projectRecord.ID), environment.IsDefaultEQ(true)).SetIsDefault(false).Save(ctx); err != nil {
			return err
		}
		if _, err := tx.ResourceProfile.Update().Where(resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.IsDefaultEQ(true)).SetIsDefault(false).Save(ctx); err != nil {
			return err
		}
	}
	if ent.IsNotFound(err) {
		environmentRecord, err = tx.Environment.Create().SetProjectID(projectRecord.ID).SetBackend(environment.BackendSSHCloud).
			SetName(name).SetImageUUID(image).SetRecipeRef(recipe).SetIsDefault(makeDefault).Save(ctx)
	} else {
		name = environmentRecord.Name
		environmentRecord, err = environmentRecord.Update().SetStatus(environment.StatusApproved).SetImageUUID(image).SetIsDefault(makeDefault).Save(ctx)
	}
	if err != nil {
		return fmt.Errorf("save Cloud SSH Environment: %w", err)
	}
	profileRecord, err := tx.ResourceProfile.Query().Where(
		resourceprofile.ProjectIDEQ(projectRecord.ID), resourceprofile.NameEQ(name), resourceprofile.BackendEQ(resourceprofile.BackendSSHCloud),
	).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if ent.IsNotFound(err) {
		_, err = tx.ResourceProfile.Create().SetProjectID(projectRecord.ID).SetBackend(resourceprofile.BackendSSHCloud).
			SetName(name).SetRegion(Backend).SetGpuNames(gpuNames).SetGpuNum(1).SetCudaFrom(1).SetCudaTo(1).
			SetCPUFrom(1).SetCPUTo(cpuLimit).SetMemoryFromGB(1).SetMemoryToGB(memoryGB).
			SetPriceFromMilli(0).SetPriceToMilli(0).SetIsDefault(makeDefault).Save(ctx)
	} else {
		_, err = profileRecord.Update().SetStatus(resourceprofile.StatusActive).SetGpuNames(gpuNames).
			SetCPUTo(cpuLimit).SetMemoryToGB(memoryGB).SetIsDefault(makeDefault).Save(ctx)
	}
	if err != nil {
		return fmt.Errorf("save Cloud SSH Resource Profile: %w", err)
	}
	_, err = tx.AuditEvent.Create().SetTenantID(tenantID).SetActorType("user").SetActorID(strings.TrimSpace(actorID)).
		SetAction("ssh_cloud.runtime_ensured").SetTargetType("project").SetTargetID(projectRecord.PublicID.String()).
		SetMetadata(map[string]any{"node_id": node.PublicID.String(), "environment_id": environmentRecord.PublicID.String(), "image": image}).Save(ctx)
	return err
}

func (s *Service) loadedView(ctx context.Context, id int) (NodeView, error) {
	record, err := s.client.CloudSSHNode.Query().Where(cloudsshnode.IDEQ(id)).
		WithProjectAccess(func(query *ent.CloudSSHProjectAccessQuery) {
			query.Where(cloudsshprojectaccess.StatusEQ(cloudsshprojectaccess.StatusActive)).WithProject()
		}).Only(ctx)
	if err != nil {
		return NodeView{}, err
	}
	runtimes, err := s.projectRuntimeViews(ctx, record)
	if err != nil {
		return NodeView{}, err
	}
	return nodeView(record, runtimes), nil
}

func (s *Service) projectRuntimeViews(ctx context.Context, node *ent.CloudSSHNode) ([]ProjectRuntimeView, error) {
	result := []ProjectRuntimeView{}
	if node == nil || len(node.Edges.ProjectAccess) == 0 {
		return result, nil
	}
	activeProjects := make(map[int]*ent.Project, len(node.Edges.ProjectAccess))
	for _, access := range node.Edges.ProjectAccess {
		if access.Status != cloudsshprojectaccess.StatusActive {
			continue
		}
		projectRecord, err := access.Edges.ProjectOrErr()
		if err != nil {
			return nil, err
		}
		activeProjects[projectRecord.ID] = projectRecord
	}
	if len(activeProjects) == 0 {
		return result, nil
	}
	records, err := s.client.Environment.Query().Where(
		environment.ProjectIDIn(mapKeys(activeProjects)...),
		environment.BackendEQ(environment.BackendSSHCloud),
		environment.RecipeRefEQ(recipePrefix+node.PublicID.String()),
		environment.StatusEQ(environment.StatusApproved),
	).Order(ent.Asc(environment.FieldName)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		projectRecord := activeProjects[record.ProjectID]
		if projectRecord == nil {
			continue
		}
		result = append(result, ProjectRuntimeView{
			ProjectID: projectRecord.PublicID.String(), ProjectName: projectRecord.Name,
			EnvironmentID: record.PublicID.String(), EnvironmentName: record.Name,
			Image: record.ImageUUID, IsDefault: record.IsDefault,
		})
	}
	return result, nil
}

func mapKeys(values map[int]*ent.Project) []int {
	result := make([]int, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}

func nodeView(record *ent.CloudSSHNode, runtimes []ProjectRuntimeView) NodeView {
	projectIDs := []string{}
	for _, access := range record.Edges.ProjectAccess {
		if access.Status != cloudsshprojectaccess.StatusActive {
			continue
		}
		if projectRecord, err := access.Edges.ProjectOrErr(); err == nil {
			projectIDs = append(projectIDs, projectRecord.PublicID.String())
		}
	}
	if runtimes == nil {
		runtimes = []ProjectRuntimeView{}
	}
	probeLog, inventory := splitProbeLog(record.Inventory)
	return NodeView{
		ID: record.PublicID.String(), Label: record.Label, Status: string(record.Status), Experimental: true, Warning: WarningOwner,
		Host: record.SSHHost, Port: record.SSHPort, User: record.SSHUser, AuthMethod: string(record.AuthMethod),
		HostKeyFingerprint: record.HostKeyFingerprint, Inventory: inventory, ProbeLog: probeLog,
		ProjectIDs: projectIDs, ProjectRuntimes: runtimes,
		CreatedActorType: record.CreatedActorType, CreatedActorID: record.CreatedActorID,
		LastProbedAt: record.LastProbedAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func assignmentView(record *ent.CloudSSHAssignment) AssignmentView {
	view := AssignmentView{ID: record.PublicID.String(), State: string(record.State), StartedAt: record.StartedAt, LastHeartbeat: record.LastHeartbeatAt, ExitCode: record.ExitCode, FailureCode: record.FailureCode}
	if experiment, err := record.Edges.ExperimentOrErr(); err == nil {
		view.ExperimentID = experiment.PublicID.String()
	}
	if node, err := record.Edges.NodeOrErr(); err == nil {
		view.NodeID = node.PublicID.String()
		view.NodeLabel = node.Label
	}
	if attempt, err := record.Edges.AttemptOrErr(); err == nil {
		view.AttemptNumber = attempt.Number
	}
	return view
}

func runtimeName(label string, publicID uuid.UUID) string {
	name := strings.Trim(runtimeNameCharacters.ReplaceAllString(strings.ToLower(strings.TrimSpace(label)), "-"), "-")
	if name == "" {
		name = "node"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return "ssh-cloud-" + name + "-" + publicID.String()[:8]
}

func hardwareLimits(raw map[string]any) (int, int) {
	inventory := inventoryFromMap(raw)
	cpuLimit := defaultCPULimit
	if inventory.CPUCount > 2 {
		cpuLimit = inventory.CPUCount - 2
	}
	if cpuLimit < 1 {
		cpuLimit = 1
	}
	if cpuLimit > 1024 {
		cpuLimit = 1024
	}
	memoryGB := defaultMemoryGB
	if inventory.MemoryBytes >= 3<<30 {
		memoryGB = int(inventory.MemoryBytes>>30) - 2
	}
	if memoryGB < 1 {
		memoryGB = 1
	}
	if memoryGB > 4096 {
		memoryGB = 4096
	}
	return cpuLimit, memoryGB
}
