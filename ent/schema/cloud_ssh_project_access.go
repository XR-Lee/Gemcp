package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CloudSSHProjectAccess authorizes one Cloud SSH node for one Project.
type CloudSSHProjectAccess struct{ ent.Schema }

func (CloudSSHProjectAccess) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (CloudSSHProjectAccess) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("node_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Enum("status").Values("active", "revoked").Default("active"),
	}
}

func (CloudSSHProjectAccess) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("cloud_ssh_project_access").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("node", CloudSSHNode.Type).Ref("project_access").Field("node_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("cloud_ssh_access").Field("project_id").Unique().Required().Immutable(),
	}
}

func (CloudSSHProjectAccess) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("node_id", "project_id").Unique(),
		index.Fields("project_id", "status"),
	}
}
