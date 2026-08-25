package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DatasetBinding names an AutoDL file-storage dataset root for a Project.
type DatasetBinding struct{ ent.Schema }

func (DatasetBinding) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (DatasetBinding) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("agent_token_id").Optional().Nillable().Immutable(),
		field.String("name").NotEmpty().MaxLen(120),
		field.Enum("backend").Values("autodl_elastic", "autodl_private"),
		field.String("canonical_root").NotEmpty().MaxLen(1024),
		field.String("environment_variable").NotEmpty().MaxLen(128).Immutable(),
		field.JSON("required_markers", []string{}).Default([]string{}),
		field.Enum("status").Values("active", "disabled").Default("active"),
	}
}

func (DatasetBinding) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("dataset_bindings").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("dataset_bindings").Field("project_id").Unique().Required().Immutable(),
		edge.From("agent_token", AgentToken.Type).Ref("dataset_bindings").Field("agent_token_id").Unique().Immutable(),
	}
}

func (DatasetBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "backend", "name").Unique(),
		index.Fields("project_id", "backend", "environment_variable").Unique(),
		index.Fields("project_id", "backend", "status"),
	}
}
