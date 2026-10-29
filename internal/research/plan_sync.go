package research

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/XR-Lee/Gemcp/ent/auditevent"
	"github.com/XR-Lee/Gemcp/ent/researchedge"
	"github.com/XR-Lee/Gemcp/internal/agentauth"
)

const planSyncMetricsDisclaimer = "Numbers below are copied from the Gemcp Graph when this patch was generated. Measured numbers match the Experiment's metrics.json; asserted numbers were typed into the Graph. Gemcp stays the source of truth; this markdown is a docs-only amendment for the protocol branch."

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
	fmt.Fprintf(&b, "\n## Hypotheses\n\n")
	if len(view.Hypotheses) == 0 {
		b.WriteString("No hypotheses are on the Graph yet.\n")
	}
	for _, hypothesis := range view.Hypotheses {
		fmt.Fprintf(&b, "### %s\n\n", oneLine(hypothesis.Title))
		if hypothesis.Summary != "" {
			fmt.Fprintf(&b, "%s\n\n", oneLine(hypothesis.Summary))
		}
		if len(hypothesis.Experiments) == 0 {
			b.WriteString("- No experiment yet.\n")
		} else {
			for _, record := range hypothesis.Experiments {
				b.WriteString("- " + planSyncRunLine(record) + "\n")
			}
		}
		for _, line := range planSyncObservationLines(view, hypothesis) {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "## Decisions\n\n")
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
			fmt.Fprintf(&b, "- %s", action.Title)
			if action.Detail != "" {
				fmt.Fprintf(&b, ": %s", oneLine(action.Detail))
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\n## Closed evidence\n\n")
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
			if value := metricText(node.MetricName, node.MetricValue, node.MetricSource); value != "" {
				fmt.Fprintf(&b, ": %s", value)
			} else if node.Status == "failed" {
				b.WriteString(": run failed, no evidence")
			}
			b.WriteString("\n")
		}
	}
	fmt.Fprintf(&b, "\n## Do not\n\n")
	b.WriteString("- Do not paste training code onto the protocol branch.\n")
	b.WriteString("- Do not force-push.\n")
	b.WriteString("- Do not treat this file as the metric ledger.\n")
	return b.String()
}

// planSyncRunLine reads as one sentence a person can scan: which run, what
// it measured, and what that means for the hypothesis.
func planSyncRunLine(record HypothesisRunRecord) string {
	line := oneLine(record.Title)
	if record.ResultTitle == "" {
		return line + ": " + strings.ReplaceAll(record.State, "_", " ") + ", not closed yet"
	}
	if record.ResultTitle != record.Title {
		line += ": " + oneLine(record.ResultTitle)
	}
	if value := metricText(record.MetricName, record.MetricValue, record.MetricSource); value != "" {
		line += ", " + value
	}
	if record.State != "" && record.State != "succeeded" {
		line += ", run " + strings.ReplaceAll(record.State, "_", " ")
	}
	verdict := record.Verdict
	if verdict == "" {
		verdict = verdictInconclusive
	}
	line += ". Verdict: " + verdict + "."
	if record.AgentReading != "" {
		line += " Agent reading: " + record.AgentReading + "."
		if title := oneLine(record.AgentReadingTitle); title != "" {
			line += " (" + title + ")"
		}
	}
	return line
}

// planSyncObservationLines lists observations that belong to this hypothesis:
// descendants stop at the next hypothesis, and a supports or contradicts
// edge into this hypothesis is included even when the observation is not a
// descendant. supports and contradicts outrank leads_to.
func planSyncObservationLines(view *StudyView, hypothesis HypothesisView) []string {
	if view == nil {
		return nil
	}
	outgoing := map[string][]EdgeView{}
	nodesByID := map[string]NodeView{}
	for _, edge := range view.Edges {
		outgoing[edge.FromID] = append(outgoing[edge.FromID], edge)
	}
	for _, node := range view.Nodes {
		nodesByID[node.ID] = node
	}
	chosen := map[string]string{}
	order := make([]string, 0)
	consider := func(node NodeView, relation string) {
		if node.Kind != "observation" || observationRelationRank(relation) > 2 {
			return
		}
		previous, ok := chosen[node.ID]
		if !ok {
			order = append(order, node.ID)
			chosen[node.ID] = relation
			return
		}
		if observationRelationRank(relation) < observationRelationRank(previous) {
			chosen[node.ID] = relation
		}
	}
	for _, node := range descendantsOf(outgoing, nodesByID, hypothesis.ID) {
		consider(node, observationPlanRelation(outgoing, hypothesis.ID, node.ID))
	}
	for _, edge := range view.Edges {
		if edge.ToID != hypothesis.ID || (edge.Relation != string(researchedge.RelationSupports) && edge.Relation != string(researchedge.RelationContradicts)) {
			continue
		}
		node, ok := nodesByID[edge.FromID]
		if !ok {
			continue
		}
		consider(node, edge.Relation)
	}
	lines := make([]string, 0, len(order))
	for _, id := range order {
		lines = append(lines, formatPlanSyncObservation(nodesByID[id], chosen[id]))
	}
	return lines
}

func observationPlanRelation(outgoing map[string][]EdgeView, hypothesisID, observationID string) string {
	best := ""
	for _, edge := range outgoing[hypothesisID] {
		if edge.ToID == observationID {
			best = preferObservationRelation(best, edge.Relation)
		}
	}
	for _, edge := range outgoing[observationID] {
		if edge.ToID == hypothesisID {
			best = preferObservationRelation(best, edge.Relation)
		}
	}
	if best == "" {
		return string(researchedge.RelationLeadsTo)
	}
	return best
}

func preferObservationRelation(current, next string) string {
	if observationRelationRank(next) > 2 {
		return current
	}
	if current == "" || observationRelationRank(next) < observationRelationRank(current) {
		return next
	}
	return current
}

func observationRelationRank(relation string) int {
	switch relation {
	case string(researchedge.RelationSupports):
		return 0
	case string(researchedge.RelationContradicts):
		return 1
	case string(researchedge.RelationLeadsTo):
		return 2
	default:
		return 3
	}
}

func formatPlanSyncObservation(node NodeView, relation string) string {
	line := "- Observation (" + relation + "): " + oneLine(node.Title)
	if summary := oneLine(node.Summary); summary != "" {
		line += ". " + summary
	}
	if value := metricText(node.MetricName, node.MetricValue, node.MetricSource); value != "" {
		line += ". " + value
	}
	return line
}

func metricText(name string, value *float64, source string) string {
	if name == "" || value == nil {
		return ""
	}
	text := name + " = " + formatMetricValue(*value)
	if source != "" {
		text += " (" + map[string]string{metricSourceExperiment: "measured", metricSourceAsserted: "asserted"}[source] + ")"
	}
	return text
}

// formatMetricValue keeps four significant digits without switching to
// exponent notation, which reads poorly in a status document.
func formatMetricValue(value float64) string {
	text := strconv.FormatFloat(value, 'g', 4, 64)
	if strings.ContainsAny(text, "eE") {
		text = strconv.FormatFloat(value, 'f', -1, 64)
	}
	return text
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
