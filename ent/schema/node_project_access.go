package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NodeProjectAccess authorizes one organization-level node for one Project.
type NodeProjectAccess struct{ ent.Schema }

func (NodeProjectAccess) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (NodeProjectAccess) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("node_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Enum("status").Values("active", "revoked").Default("active"),
		field.Enum("execution_policy").Values("strict", "trusted_workspace").Default("strict"),
		field.String("workspace_path").Optional().Nillable().MaxLen(4096),
		field.Strings("successful_images").Optional().Default([]string{}),
	}
}

func (NodeProjectAccess) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("node_project_access").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("node", SelfHostedNode.Type).Ref("project_access").Field("node_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("node_access").Field("project_id").Unique().Required().Immutable(),
	}
}

func (NodeProjectAccess) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("node_id", "project_id").Unique(),
		index.Fields("project_id", "status"),
	}
}
