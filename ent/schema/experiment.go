package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Experiment struct{ ent.Schema }

func (Experiment) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Experiment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("agent_token_id").Optional().Nillable().Immutable(),
		field.Int("repository_id").Optional().Nillable().Immutable(),
		field.Int("environment_id").Immutable(),
		field.Int("resource_profile_id").Immutable(),
		field.String("state").Default("queued").
			Validate(enum("queued", "provisioning", "running", "cancelling", "collecting", "succeeded", "failed", "cancelled", "timed_out", "budget_stopped", "provider_error")),
		field.String("desired_state").Default("running").Validate(enum("running", "cancelled")),
		field.String("commit_sha").Immutable().MaxLen(64),
		field.Enum("execution_mode").Values("shell", "argv").Default("shell").Immutable(),
		field.Strings("argv").Optional().Immutable(),
		field.Text("command").Immutable(),
		field.Int("max_runtime_seconds").Immutable().Positive(),
		field.Int("timeout_extension_seconds").Immutable().NonNegative(),
		field.Int("termination_grace_seconds").Immutable().NonNegative(),
		field.JSON("repository_snapshot", map[string]any{}).Immutable(),
		field.JSON("environment_snapshot", map[string]any{}).Immutable(),
		field.JSON("resource_snapshot", map[string]any{}).Immutable(),
		field.Strings("secret_names").Default([]string{}).Immutable(),
		field.String("output_path").Immutable().NotEmpty(),
		field.Int64("reserved_cost_milli").Immutable().NonNegative(),
		field.Int64("estimated_cost_milli").Default(0).NonNegative(),
		field.String("provider_resource_id").Optional().Nillable().MaxLen(255),
		field.String("provider_status").Optional().Nillable().MaxLen(255),
		field.Time("started_at").Optional().Nillable(),
		field.Time("deadline_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Int("exit_code").Optional().Nillable(),
		field.String("failure_code").Optional().Nillable().MaxLen(100),
		field.Text("failure_reason").Optional().Nillable(),
		field.Text("log_tail").Optional().Nillable(),
		field.JSON("metrics", map[string]any{}).Default(map[string]any{}),
		field.Time("cancel_requested_at").Optional().Nillable(),
		field.Time("timeout_extended_at").Optional().Nillable(),
		field.Time("budget_finalized_at").Optional().Nillable(),
		field.Time("lease_expires_at").Optional().Nillable(),
		field.String("lease_owner").Optional().Nillable().MaxLen(255),
		field.Time("next_attempt_at").Default(time.Now),
	}
}

func (Experiment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("experiments").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("experiments").Field("project_id").Unique().Required().Immutable(),
		edge.From("agent_token", AgentToken.Type).Ref("experiments").Field("agent_token_id").Unique().Immutable(),
		edge.From("repository", Repository.Type).Ref("experiments").Field("repository_id").Unique().Immutable(),
		edge.From("environment", Environment.Type).Ref("experiments").Field("environment_id").Unique().Required().Immutable(),
		edge.From("resource_profile", ResourceProfile.Type).Ref("experiments").Field("resource_profile_id").Unique().Required().Immutable(),
		edge.To("attempts", Attempt.Type),
		edge.To("provider_resources", ProviderResource.Type),
		edge.To("node_assignments", NodeAssignment.Type),
		edge.To("cloud_ssh_assignments", CloudSSHAssignment.Type),
		edge.To("budget_entries", BudgetEntry.Type),
		edge.To("idempotency_records", IdempotencyRecord.Type),
		edge.To("diagnostic_run", DiagnosticRun.Type).Unique(),
		edge.To("proposal", ExperimentProposal.Type).Unique(),
		edge.To("research_nodes", ResearchNode.Type),
	}
}

func (Experiment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "state", "created_at"),
		index.Fields("state", "next_attempt_at", "created_at"),
		index.Fields("provider_resource_id").Unique(),
	}
}
