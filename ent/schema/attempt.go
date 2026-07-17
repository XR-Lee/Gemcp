package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Attempt struct{ ent.Schema }

func (Attempt) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Attempt) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("experiment_id").Immutable(),
		field.Int("number").Immutable().Positive(),
		field.String("state").Default("starting").Validate(enum("starting", "running", "succeeded", "failed", "cancelled")),
		field.String("provider_resource_id").Optional().Nillable().MaxLen(255),
		field.String("retry_reason").Optional().Nillable().MaxLen(255),
		field.String("failure_code").Optional().Nillable().MaxLen(100),
		field.Text("failure_reason").Optional().Nillable(),
		field.Time("started_at").Optional().Nillable(),
		field.Time("finished_at").Optional().Nillable(),
		field.Int64("estimated_cost_milli").Default(0).NonNegative(),
	}
}

func (Attempt) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("attempts").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("attempts").Field("project_id").Unique().Required().Immutable(),
		edge.From("experiment", Experiment.Type).Ref("attempts").Field("experiment_id").Unique().Required().Immutable(),
	}
}

func (Attempt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("experiment_id", "number").Unique(),
		index.Fields("project_id", "state", "created_at"),
	}
}
