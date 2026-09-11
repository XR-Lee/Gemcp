package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ExperimentProposal struct{ ent.Schema }

func (ExperimentProposal) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ExperimentProposal) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("agent_token_id").Optional().Nillable().Immutable(),
		field.Int("repository_id").Optional().Nillable().Immutable(),
		field.Int("environment_id").Immutable(),
		field.Int("resource_profile_id").Immutable(),
		field.Int("experiment_id").Optional().Nillable(),
		field.Enum("status").Values("prepared", "submitted").Default("prepared"),
		field.String("requested_ref").NotEmpty().MaxLen(255).Immutable(),
		field.String("commit_sha").NotEmpty().MaxLen(64).Immutable(),
		field.Enum("execution_mode").Values("argv").Immutable(),
		field.Strings("argv").Immutable(),
		field.Text("display_command").Immutable(),
		field.String("runtime_preset").Default("smoke").MaxLen(80).Immutable(),
		field.Int("max_runtime_seconds").Positive().Immutable(),
		field.Int("timeout_extension_seconds").NonNegative().Immutable(),
		field.Int("termination_grace_seconds").NonNegative().Immutable(),
		field.JSON("project_snapshot", map[string]any{}).Immutable(),
		field.JSON("repository_snapshot", map[string]any{}).Immutable(),
		field.JSON("environment_snapshot", map[string]any{}).Immutable(),
		field.JSON("resource_snapshot", map[string]any{}).Immutable(),
		field.JSON("checks", []map[string]any{}).Immutable(),
		field.Int64("reserved_cost_milli").NonNegative().Immutable(),
		field.String("confirmation_digest").NotEmpty().MaxLen(80).Immutable(),
		field.Time("expires_at").Immutable(),
		field.Time("submitted_at").Optional().Nillable(),
	}
}

func (ExperimentProposal) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("experiment_proposals").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("experiment_proposals").Field("project_id").Unique().Required().Immutable(),
		edge.From("agent_token", AgentToken.Type).Ref("experiment_proposals").Field("agent_token_id").Unique().Immutable(),
		edge.From("repository", Repository.Type).Ref("experiment_proposals").Field("repository_id").Unique().Immutable(),
		edge.From("environment", Environment.Type).Ref("experiment_proposals").Field("environment_id").Unique().Required().Immutable(),
		edge.From("resource_profile", ResourceProfile.Type).Ref("experiment_proposals").Field("resource_profile_id").Unique().Required().Immutable(),
		edge.From("experiment", Experiment.Type).Ref("proposal").Field("experiment_id").Unique(),
	}
}

func (ExperimentProposal) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "created_at"),
		index.Fields("agent_token_id", "status", "expires_at"),
	}
}
