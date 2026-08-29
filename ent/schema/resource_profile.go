package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ResourceProfile struct{ ent.Schema }

func (ResourceProfile) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ResourceProfile) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id").Immutable(),
		field.Enum("backend").Values("autodl_private", "autodl_elastic", "self_hosted").Default("autodl_private").Immutable(),
		field.String("name").NotEmpty().MaxLen(120),
		field.String("region").NotEmpty().MaxLen(80),
		field.Strings("gpu_names"),
		field.Int("gpu_num").Positive().Default(1),
		field.Int("cuda_from").Positive(),
		field.Int("cuda_to").Positive(),
		field.Int("cpu_from").Positive(),
		field.Int("cpu_to").Positive(),
		field.Int("memory_from_gb").Positive(),
		field.Int("memory_to_gb").Positive(),
		field.Int64("price_from_milli").NonNegative(),
		field.Int64("price_to_milli").NonNegative(),
		field.Bool("reuse_container").Default(false),
		field.Bool("is_default").Default(false),
		field.Enum("status").Values("active", "disabled").Default("active"),
	}
}

func (ResourceProfile) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("resource_profiles").Field("project_id").Unique().Required().Immutable(),
		edge.To("experiments", Experiment.Type),
		edge.To("experiment_proposals", ExperimentProposal.Type),
	}
}

func (ResourceProfile) Indexes() []ent.Index {
	return []ent.Index{index.Fields("project_id", "name").Unique()}
}
