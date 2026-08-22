package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Study is a research question inside one Project.
type Study struct{ ent.Schema }

func (Study) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Study) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("repository_id").Optional().Nillable(),
		field.Int("agent_token_id").Optional().Nillable().Immutable(),
		field.String("name").NotEmpty().MaxLen(120),
		field.Text("question").NotEmpty(),
		field.Text("summary").Optional(),
		field.Enum("status").Values("active", "paused", "archived").Default("active"),
	}
}

func (Study) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("studies").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("studies").Field("project_id").Unique().Required().Immutable(),
		edge.From("repository", Repository.Type).Ref("studies").Field("repository_id").Unique(),
		edge.From("agent_token", AgentToken.Type).Ref("studies").Field("agent_token_id").Unique().Immutable(),
		edge.To("iteration_plans", IterationPlan.Type),
		edge.To("research_nodes", ResearchNode.Type),
		edge.To("research_edges", ResearchEdge.Type),
	}
}

func (Study) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "name").Unique(),
		index.Fields("project_id", "status", "updated_at"),
	}
}
