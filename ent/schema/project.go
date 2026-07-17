package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Project struct{ ent.Schema }

func (Project) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Project) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.String("name").NotEmpty().MaxLen(120),
		field.String("slug").NotEmpty().MaxLen(80),
		field.Enum("status").Values("active", "paused", "archived").Default("active"),
		field.Int64("monthly_budget_milli").NonNegative(),
		field.Int64("max_experiment_milli").NonNegative(),
		field.Int("max_concurrency").Positive().Default(1),
		field.Int("max_runtime_seconds").Positive().Default(86400),
		field.Int("timeout_extension_seconds").NonNegative().Default(3600),
		field.Int("termination_grace_seconds").NonNegative().Default(60),
		field.String("timezone").Default("Asia/Shanghai").MaxLen(64),
	}
}

func (Project) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("projects").Field("tenant_id").Unique().Required().Immutable(),
		edge.To("environments", Environment.Type),
		edge.To("resource_profiles", ResourceProfile.Type),
		edge.To("repositories", Repository.Type),
		edge.To("agent_tokens", AgentToken.Type),
		edge.To("experiments", Experiment.Type),
		edge.To("attempts", Attempt.Type),
		edge.To("provider_resources", ProviderResource.Type),
		edge.To("budget_entries", BudgetEntry.Type),
	}
}

func (Project) Indexes() []ent.Index {
	return []ent.Index{index.Fields("tenant_id", "slug").Unique()}
}
