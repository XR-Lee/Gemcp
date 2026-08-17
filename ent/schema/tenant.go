package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Tenant struct{ ent.Schema }

func (Tenant) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Tenant) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().MaxLen(120),
		field.String("status").Default("active").MaxLen(32),
	}
}

func (Tenant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("users", User.Type),
		edge.To("provider_accounts", ProviderAccount.Type),
		edge.To("projects", Project.Type),
		edge.To("experiments", Experiment.Type),
		edge.To("attempts", Attempt.Type),
		edge.To("provider_resources", ProviderResource.Type),
		edge.To("budget_entries", BudgetEntry.Type),
		edge.To("idempotency_records", IdempotencyRecord.Type),
		edge.To("audit_events", AuditEvent.Type),
		edge.To("notification_settings", NotificationSetting.Type),
		edge.To("notifications", Notification.Type),
		edge.To("self_hosted_nodes", SelfHostedNode.Type),
		edge.To("node_enrollments", NodeEnrollment.Type),
		edge.To("node_project_access", NodeProjectAccess.Type),
		edge.To("node_commands", NodeCommand.Type),
		edge.To("node_events", NodeEvent.Type),
		edge.To("node_assignments", NodeAssignment.Type),
		edge.To("diagnostic_runs", DiagnosticRun.Type),
		edge.To("experiment_proposals", ExperimentProposal.Type),
		edge.To("workspace_datasets", WorkspaceDataset.Type),
		edge.To("studies", Study.Type),
		edge.To("iteration_plans", IterationPlan.Type),
		edge.To("research_nodes", ResearchNode.Type),
		edge.To("research_edges", ResearchEdge.Type),
	}
}
