package research

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
)

const planSyncMetricsDisclaimer = "Metrics and scalars live on the Gemcp Graph. This markdown is a docs-only amendment for the protocol branch, not the source of truth."

type PlanSyncInput struct {
	StudyID    string `json:"study_id,omitempty" jsonschema:"Study ID; omit when the Project has exactly one Study"`
	DryRun     *bool  `json:"dry_run,omitempty" jsonschema:"default true. true returns the patch only. false writes an audit receipt. Gemcp never pushes git"`
	TargetPath string `json:"target_path,omitempty" jsonschema:"optional markdown path on the protocol branch; defaults to the Study protocol_doc_path or research-plan/GEMCP-SYNC.md"`
}

type PlanSyncExport struct {
	StudyID      string    `json:"study_id"`
	DryRun       bool      `json:"dry_run"`
	TargetBranch string    `json:"target_branch"`
	TargetPath   string    `json:"target_path"`
	Title        string    `json:"title"`
	Markdown     string    `json:"markdown"`
	Warning      string    `json:"warning,omitempty"`
	Recorded     bool      `json:"recorded"`
	GeneratedAt  time.Time `json:"generated_at"`
}

func planSyncDryRun(input PlanSyncInput) bool {
	if input.DryRun == nil {
		return true
	}
	return *input.DryRun
}

func (s *Service) AgentExportPlanSync(ctx context.Context, principal agentauth.Principal, input PlanSyncInput) (PlanSyncExport, error) {
	dryRun := planSyncDryRun(input)
	if dryRun && !principal.HasScope("read") {
		return PlanSyncExport{}, ErrForbidden
	}
	if !dryRun && !principal.HasScope("submit") {
		return PlanSyncExport{}, ErrForbidden
	}
	tokenID := principal.TokenID
	current := actor{
		tenantID: principal.TenantID, projectID: principal.ProjectID, projectPublic: principal.ProjectPublicID,
		actorType: auditevent.ActorTypeAgentToken, actorID: principal.TokenPublicID,
	}
	if !dryRun && tokenID != 0 {
		current.tokenID = &tokenID
	}
	return s.exportPlanSync(ctx, current, input, dryRun)
}

func (s *Service) OwnerExportPlanSync(ctx context.Context, tenantID int, actorID, projectPublicID string, input PlanSyncInput) (PlanSyncExport, error) {
	projectRecord, err := s.project(ctx, tenantID, projectPublicID)
	if err != nil {
		return PlanSyncExport{}, err
	}
	return s.exportPlanSync(ctx, actor{
		tenantID: tenantID, projectID: projectRecord.ID, projectPublic: projectRecord.PublicID.String(),
		actorType: auditevent.ActorTypeUser, actorID: actorID,
	}, input, planSyncDryRun(input))
}

func (s *Service) exportPlanSync(ctx context.Context, current actor, input PlanSyncInput, dryRun bool) (PlanSyncExport, error) {
	workspace, err := s.workspace(ctx, current, input.StudyID)
	if err != nil {
		return PlanSyncExport{}, err
	}
	if workspace.Study == nil {
		return PlanSyncExport{}, invalid("create a Study before exporting a research-plan patch")
	}
	targetPath, err := resolvePlanSyncPath(workspace.Study, input.TargetPath)
	if err != nil {
		return PlanSyncExport{}, err
	}
	targetBranch := defaultProtocolBranch
	if workspace.Study.Route != nil && strings.TrimSpace(workspace.Study.Route.ProtocolBranch) != "" {
		targetBranch = workspace.Study.Route.ProtocolBranch
	}
	generatedAt := time.Now().UTC().Truncate(time.Second)
	title := "Gemcp research-plan amendment: " + workspace.Study.Name
	markdown := renderPlanSyncMarkdown(workspace, targetBranch, targetPath, generatedAt)
	result := PlanSyncExport{
		StudyID: workspace.Study.ID, DryRun: dryRun, TargetBranch: targetBranch, TargetPath: targetPath,
		Title: title, Markdown: markdown, GeneratedAt: generatedAt,
		Warning: "Gemcp does not push this patch. Apply it on the docs-only protocol branch. Do not force-push. Do not write training code or frozen recipe files.",
	}
	if dryRun {
		return result, nil
	}
	if err := s.recordPlanSyncExport(ctx, current, result); err != nil {
		return PlanSyncExport{}, err
	}
	result.Recorded = true
	return result, nil
}

func resolvePlanSyncPath(view *StudyView, requested string) (string, error) {
	path := strings.TrimSpace(requested)
	if path == "" && view != nil && view.Route != nil {
		path = strings.TrimSpace(view.Route.ProtocolDocPath)
	}
	if path == "" {
		path = defaultPlanSyncPath
	}
	normalized, err := normalizeProtocolDocPath(path)
	if err != nil {
		return "", err
	}
	if normalized == "" {
		return "", invalid("target_path is required")
	}
	return normalized, nil
}

func (s *Service) recordPlanSyncExport(ctx context.Context, current actor, export PlanSyncExport) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeAudit(ctx, tx, current, "research.plan_sync_exported", "study", export.StudyID, map[string]any{
		"target_branch": export.TargetBranch, "target_path": export.TargetPath, "dry_run": false,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func renderPlanSyncMarkdown(workspace Workspace, targetBranch, targetPath string, generatedAt time.Time) string {
	view := workspace.Study
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", "Gemcp research-plan amendment")
	fmt.Fprintf(&b, "- Study: %s\n", view.Name)
	fmt.Fprintf(&b, "- Question: %s\n", oneLine(view.Question))
	if view.Route != nil {
		if view.Route.ProtocolBranch != "" {
			fmt.Fprintf(&b, "- Protocol branch: `%s`\n", view.Route.ProtocolBranch)
		}
		if view.Route.ProtocolDocPath != "" {
			fmt.Fprintf(&b, "- Protocol doc: `%s`\n", view.Route.ProtocolDocPath)
		}
		if view.Route.CodeRefPattern != "" {
			fmt.Fprintf(&b, "- Allowed code refs: `%s`\n", view.Route.CodeRefPattern)
		}
	}
	if view.Repository != nil {
		fmt.Fprintf(&b, "- Repository: `%s` (default branch `%s` is not a live experiment ref)\n", view.Repository.SSHURL, view.Repository.DefaultBranch)
	}
	fmt.Fprintf(&b, "- Target: `%s` on `%s`\n", targetPath, targetBranch)
	fmt.Fprintf(&b, "- Generated at: %s\n", generatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "\n%s\n", planSyncMetricsDisclaimer)
	fmt.Fprintf(&b, "\n## Decisions\n\n")
	decisions := nodesByKind(view.Nodes, "decision")
	if len(decisions) == 0 {
		b.WriteString("No decision nodes are on the Graph yet.\n")
	} else {
		for _, node := range decisions {
			fmt.Fprintf(&b, "- %s", node.Title)
			if node.Summary != "" {
				fmt.Fprintf(&b, ": %s", oneLine(node.Summary))
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\n## Next actions\n\n")
	if len(workspace.NextActions) == 0 {
		b.WriteString("No next actions.\n")
	} else {
		for _, action := range workspace.NextActions {
			if action.Kind == "export_plan_sync" {
				continue
			}
			fmt.Fprintf(&b, "- %s (%s)", action.Title, action.Tool)
			if action.Detail != "" {
				fmt.Fprintf(&b, ": %s", oneLine(action.Detail))
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\n## Closed evidence (titles only)\n\n")
	results := nodesByKind(view.Nodes, "result")
	if len(results) == 0 {
		b.WriteString("No closed results on the Graph.\n")
	} else {
		for _, node := range results {
			fmt.Fprintf(&b, "- %s", node.Title)
			if node.Branch != "" {
				fmt.Fprintf(&b, " on `%s`", node.Branch)
			}
			if node.CommitSHA != "" {
				fmt.Fprintf(&b, " @ `%s`", node.CommitSHA)
			}
			b.WriteString(". Scalar values stay on the Graph.\n")
		}
	}
	fmt.Fprintf(&b, "\n## Do not\n\n")
	b.WriteString("- Do not paste training code onto the protocol branch.\n")
	b.WriteString("- Do not force-push.\n")
	b.WriteString("- Do not treat this file as the metric ledger.\n")
	return b.String()
}

func nodesByKind(nodes []NodeView, kind string) []NodeView {
	out := make([]NodeView, 0)
	for _, node := range nodes {
		if node.Kind == kind {
			out = append(out, node)
		}
	}
	return out
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
