package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// IterationPlan is an immutable snapshot of the next research actions for a Study.
type IterationPlan struct{ ent.Schema }

func (IterationPlan) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (IterationPlan) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("study_id").Immutable(),
		field.Int("agent_token_id").Optional().Nillable().Immutable(),
		field.Enum("status").Values("active", "completed", "superseded").Default("active"),
		field.Text("goal").NotEmpty(),
		field.Text("next_action").NotEmpty(),
		field.Text("rationale").Optional(),
		field.JSON("steps", []map[string]any{}).Default([]map[string]any{}),
	}
}

func (IterationPlan) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("iteration_plans").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("iteration_plans").Field("project_id").Unique().Required().Immutable(),
		edge.From("study", Study.Type).Ref("iteration_plans").Field("study_id").Unique().Required().Immutable(),
		edge.From("agent_token", AgentToken.Type).Ref("iteration_plans").Field("agent_token_id").Unique().Immutable(),
	}
}

func (IterationPlan) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("study_id", "status", "created_at"),
		index.Fields("project_id", "created_at"),
	}
}
