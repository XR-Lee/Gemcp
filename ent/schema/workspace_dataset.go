package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WorkspaceDataset names data already contained by an Owner-approved trusted workspace.
type WorkspaceDataset struct{ ent.Schema }

func (WorkspaceDataset) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (WorkspaceDataset) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("node_id").Immutable(),
		field.Int("agent_token_id").Optional().Nillable().Immutable(),
		field.String("name").NotEmpty().MaxLen(120),
		field.String("relative_path").NotEmpty().MaxLen(1024),
		field.String("environment_variable").NotEmpty().MaxLen(128).Immutable(),
		field.Enum("status").Values("active", "disabled").Default("active"),
	}
}

func (WorkspaceDataset) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("workspace_datasets").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("workspace_datasets").Field("project_id").Unique().Required().Immutable(),
		edge.From("node", SelfHostedNode.Type).Ref("workspace_datasets").Field("node_id").Unique().Required().Immutable(),
		edge.From("agent_token", AgentToken.Type).Ref("workspace_datasets").Field("agent_token_id").Unique().Immutable(),
	}
}

func (WorkspaceDataset) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "node_id", "name").Unique(),
		index.Fields("project_id", "node_id", "relative_path").Unique(),
		index.Fields("project_id", "node_id", "environment_variable").Unique(),
		index.Fields("project_id", "status"),
	}
}
