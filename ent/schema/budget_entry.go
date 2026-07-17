package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type BudgetEntry struct{ ent.Schema }

func (BudgetEntry) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (BudgetEntry) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("experiment_id").Immutable(),
		field.String("period").Immutable().MinLen(7).MaxLen(7),
		field.String("kind").Immutable().Validate(enum("reservation", "release", "charge", "adjustment")),
		field.Int64("amount_milli").Immutable(),
		field.String("description").Immutable().MaxLen(255),
	}
}

func (BudgetEntry) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("budget_entries").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("budget_entries").Field("project_id").Unique().Required().Immutable(),
		edge.From("experiment", Experiment.Type).Ref("budget_entries").Field("experiment_id").Unique().Required().Immutable(),
	}
}

func (BudgetEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "period", "created_at"),
		index.Fields("experiment_id", "kind"),
	}
}
