package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Environment struct{ ent.Schema }

func (Environment) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Environment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id").Immutable(),
		field.Enum("backend").Values("autodl_private", "autodl_elastic", "self_hosted", "ssh_cloud").Default("autodl_private").Immutable(),
		field.String("name").NotEmpty().MaxLen(120),
		field.String("image_uuid").NotEmpty().MaxLen(512),
		field.String("recipe_ref").Optional().MaxLen(512),
		field.Enum("status").Values("pending", "approved", "disabled").Default("approved"),
		field.Bool("is_default").Default(false),
	}
}

func (Environment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("environments").Field("project_id").Unique().Required().Immutable(),
		edge.To("experiments", Experiment.Type),
		edge.To("experiment_proposals", ExperimentProposal.Type),
	}
}

func (Environment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique(),
		index.Fields("project_id", "image_uuid"),
	}
}
