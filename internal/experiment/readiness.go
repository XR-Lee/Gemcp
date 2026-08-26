package experiment

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/agenttoken"
	"github.com/XR-Lee/Gemcp/ent/cloudsshnode"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
)

const (
	ReadinessReady           = "ready"
	ReadinessBlocked         = "blocked"
	ReadinessWaitingAgent    = "waiting_agent"
	ReadinessWaitingCompute  = "waiting_compute"
	readinessInspectTool     = "get_project_options"
	readinessMonitorTool     = "get_experiment"
	maxListedReadinessTokens = 200
)

func readinessHeartbeatNote() string {
	return "MCP last_used_at updates on every authenticated tool call. Self-hosted last_seen_at is the gemcp-node heartbeat. Cloud SSH last_probed_at updates when get_project_options probes or the Owner probes. Monitor running work with get_experiment. Do not SSH for logs or invent a private heartbeat."
}

func readinessBindingNote() string {
	return "Agents are Project-scoped. They are not exclusively bound to one node. Cloud SSH nodes registered by an Agent are attributed on that Project. Self-hosted nodes appear after Owner authorization. Do not invent a host or fall back to another backend."
}

func projectOptionsReadiness(options ProjectOptions) *OptionsReadiness {
	ready, blocked := computeCounts(options.SelfHostedNodes, options.SSHCloudNodes)
	status, summary := computeStatus(1, ready, blocked, ready+blocked)
	if status == ReadinessWaitingAgent {
		status = ReadinessWaitingCompute
		summary = "No authorized compute is visible yet."
	}
	return &OptionsReadiness{
		Status: status, Summary: summary, ReadyCompute: ready, BlockedCompute: blocked,
		Heartbeat: OptionsHeartbeat{InspectTool: readinessInspectTool, MonitorTool: readinessMonitorTool, Note: readinessHeartbeatNote()},
	}
}

func computeCounts(selfHosted []SelfHostedNodeOption, sshCloud []SSHCloudNodeOption) (ready, blocked int) {
	for _, node := range selfHosted {
		if node.Ready {
			ready++
		} else {
			blocked++
		}
	}
	for _, node := range sshCloud {
		if node.Ready {
			ready++
		} else {
			blocked++
		}
	}
	return ready, blocked
}

func computeStatus(activeAgents, ready, blocked, total int) (string, string) {
	if activeAgents == 0 {
		return ReadinessWaitingAgent, "No active Agent Token. Create a handshake so an Agent can see this Project."
	}
	if total == 0 {
		return ReadinessWaitingCompute, "An Agent is bound to the Project, but no Self-hosted or Cloud SSH node is visible yet."
	}
	if ready > 0 {
		return ReadinessReady, fmt.Sprintf("%d Agent(s) and %d ready compute target(s). Binding is Project-scoped, not exclusive to one node.", activeAgents, ready)
	}
	if blocked > 0 {
		return ReadinessBlocked, "Agents can see compute, but every target has a readiness blocker."
	}
	return ReadinessWaitingCompute, "An Agent is bound to the Project, but no Self-hosted or Cloud SSH node is visible yet."
}

func (s *Service) OwnerReadiness(ctx context.Context, tenantID int, projectPublicID string) (AgentReadiness, error) {
	var result AgentReadiness
	principal, err := s.ownerPrincipal(ctx, tenantID, projectPublicID)
	if err != nil {
		return result, err
	}
	options, err := s.projectOptions(ctx, principal, false)
	if err != nil {
		return result, err
	}
	now := s.now().UTC()
	agents, err := s.readinessAgents(ctx, principal, now)
	if err != nil {
		return result, err
	}
	sshNodes, err := s.readinessSSHCloud(ctx, principal, options.SSHCloudNodes, agents)
	if err != nil {
		return result, err
	}
	selfHosted := options.SelfHostedNodes
	if selfHosted == nil {
		selfHosted = []SelfHostedNodeOption{}
	}
	activeAgents := 0
	var agentLastUsed *time.Time
	canOperate := false
	for _, agent := range agents {
		if agent.Status == "active" {
			activeAgents++
		}
		if agent.CanOperateNodes {
			canOperate = true
		}
		agentLastUsed = latestTime(agentLastUsed, agent.LastUsedAt)
	}
	ready, blocked := computeCounts(selfHosted, options.SSHCloudNodes)
	status, summary := computeStatus(activeAgents, ready, blocked, ready+blocked)
	var sshProbed, selfSeen *time.Time
	for _, node := range sshNodes {
		sshProbed = latestTime(sshProbed, node.LastProbedAt)
	}
	for _, node := range selfHosted {
		selfSeen = latestTime(selfSeen, node.LastSeenAt)
	}
	result = AgentReadiness{
		ProjectID: options.Project.ID, ProjectName: options.Project.Name, Status: status, Summary: summary,
		GeneratedAt: now, Agents: agents,
		Compute: AgentReadinessCompute{
			SSHCloudEnabled: s.proposalConfig.SSHCloudEnabled, SSHCloud: sshNodes, SelfHosted: selfHosted,
		},
		Heartbeats: AgentReadinessHeartbeats{
			AgentLastUsedAt: agentLastUsed, SSHCloudLastProbedAt: sshProbed, SelfHostedLastSeenAt: selfSeen,
			Note: readinessHeartbeatNote(),
		},
		NextActions: readinessActions(status, s.proposalConfig.SSHCloudEnabled, canOperate, len(sshNodes), sshNodes, selfHosted),
		Instructions: AgentReadinessInstructions{
			InspectTool: readinessInspectTool, MonitorTool: readinessMonitorTool,
			Heartbeat: readinessHeartbeatNote(), Binding: readinessBindingNote(),
		},
	}
	return result, nil
}

func (s *Service) readinessAgents(ctx context.Context, principal agentauth.Principal, now time.Time) ([]AgentReadinessAgent, error) {
	records, err := s.client.AgentToken.Query().Where(agenttoken.ProjectIDEQ(principal.ProjectID)).
		Order(ent.Desc(agenttoken.FieldCreatedAt)).Limit(maxListedReadinessTokens).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]AgentReadinessAgent, 0, len(records))
	for _, record := range records {
		status := string(record.Status)
		if record.Status == agenttoken.StatusActive && record.ExpiresAt != nil && !record.ExpiresAt.After(now) {
			status = "expired"
		}
		result = append(result, AgentReadinessAgent{
			ID: record.PublicID.String(), Label: record.Label, Prefix: record.Prefix,
			Scopes: append([]string(nil), record.Scopes...), Status: status, LastUsedAt: record.LastUsedAt,
			CanRead: slices.Contains(record.Scopes, "read"), CanSubmit: slices.Contains(record.Scopes, "submit"),
			CanOperateNodes: slices.Contains(record.Scopes, "operate_nodes"), BoundNodeIDs: []string{},
		})
	}
	return result, nil
}

func (s *Service) readinessSSHCloud(ctx context.Context, principal agentauth.Principal, options []SSHCloudNodeOption, agents []AgentReadinessAgent) ([]SSHCloudReadinessNode, error) {
	result := make([]SSHCloudReadinessNode, 0, len(options))
	if !s.proposalConfig.SSHCloudEnabled {
		return result, nil
	}
	records, err := s.client.CloudSSHNode.Query().Where(
		cloudsshnode.TenantIDEQ(principal.TenantID), cloudsshnode.StatusNEQ(cloudsshnode.StatusRevoked),
	).All(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]*ent.CloudSSHNode{}
	for _, record := range records {
		byID[record.PublicID.String()] = record
	}
	labelByActor := map[string]string{}
	for _, agent := range agents {
		labelByActor[agent.ID] = agent.Label
	}
	boundByActor := map[string][]string{}
	for _, option := range options {
		node := SSHCloudReadinessNode{SSHCloudNodeOption: option}
		if record := byID[option.ID]; record != nil {
			node.RegisteredByKind = record.CreatedActorType
			node.RegisteredByID = record.CreatedActorID
			if record.CreatedActorType == "agent" {
				if label := labelByActor[record.CreatedActorID]; label != "" {
					node.RegisteredByLabel = label
					boundByActor[record.CreatedActorID] = append(boundByActor[record.CreatedActorID], option.ID)
				}
			} else if record.CreatedActorType == "user" {
				node.RegisteredByLabel = "Owner"
			}
		}
		result = append(result, node)
	}
	for index := range agents {
		if ids := boundByActor[agents[index].ID]; len(ids) > 0 {
			agents[index].BoundNodeIDs = ids
		}
	}
	return result, nil
}

func readinessActions(status string, sshEnabled, canOperate bool, sshCount int, sshNodes []SSHCloudReadinessNode, selfHosted []SelfHostedNodeOption) []ReadinessAction {
	actions := make([]ReadinessAction, 0, 5)
	actions = append(actions, ReadinessAction{
		Kind: "copy_readiness", Title: "Copy readiness prompt",
		Detail: "Tell the Agent to call get_project_options, list the current Project binding, and explain heartbeats.",
	})
	if status == ReadinessWaitingAgent {
		actions = append(actions, ReadinessAction{
			Kind: "handshake", Title: "Create handshake",
			Detail: "Issue an MCP setup link so an Agent can inspect this Project.",
		})
	}
	if sshEnabled && !canOperate && sshCount == 0 {
		actions = append(actions, ReadinessAction{
			Kind: "grant_operate_nodes", Title: "Grant operate_nodes",
			Detail: "An Agent needs operate_nodes to register a Cloud SSH host. This scope is off by default.",
		})
	}
	if sshEnabled && sshCount == 0 {
		actions = append(actions, ReadinessAction{
			Kind: "register_node", Title: "Register a Cloud SSH host",
			Detail: "Have the Agent call register_ssh_cloud_node, or register the host on the Nodes page.",
		})
	}
	needsNodes := false
	for _, node := range sshNodes {
		if slices.Contains(node.Blockers, "host_key_changed") || slices.Contains(node.Blockers, "node_not_active") || slices.Contains(node.Blockers, "runtime_configuration_required") {
			needsNodes = true
		}
	}
	for _, node := range selfHosted {
		if slices.Contains(node.Blockers, "runtime_configuration_required") || slices.Contains(node.Blockers, "node_stale") || slices.Contains(node.Blockers, "node_not_online") {
			needsNodes = true
		}
	}
	if needsNodes || status == ReadinessBlocked {
		actions = append(actions, ReadinessAction{
			Kind: "open_nodes", Title: "Open Nodes",
			Detail: "Probe, revoke, or finish runtime configuration. Do not ask the Agent to SSH for a workaround.",
		})
	}
	return actions
}

func latestTime(current, next *time.Time) *time.Time {
	if next == nil {
		return current
	}
	if current == nil || next.After(*current) {
		return next
	}
	return current
}
