package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProjectWorkload stores a reviewed one-shot argv as a named Project workload.
type ProjectWorkload struct{ ent.Schema }

func (ProjectWorkload) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ProjectWorkload) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("source_experiment_id").Immutable(),
		field.String("name").NotEmpty().MaxLen(64).Immutable(),
		field.Text("manifest_yaml").NotEmpty().Immutable(),
		field.Strings("entrypoint").Immutable(),
		field.String("runtime_preset").Optional().MaxLen(80).Immutable(),
		field.String("dataset").Optional().MaxLen(64).Immutable(),
		field.String("working_directory").Optional().MaxLen(255).Immutable(),
	}
}

func (ProjectWorkload) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("project_workloads").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("project_workloads").Field("project_id").Unique().Required().Immutable(),
		edge.From("source_experiment", Experiment.Type).Ref("saved_workload").Field("source_experiment_id").Unique().Required().Immutable(),
	}
}

func (ProjectWorkload) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique(),
		index.Fields("project_id", "created_at"),
	}
}
