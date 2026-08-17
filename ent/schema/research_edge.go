package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ResearchEdge connects two Graph nodes inside one Study.
type ResearchEdge struct{ ent.Schema }

func (ResearchEdge) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ResearchEdge) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("study_id").Immutable(),
		field.Int("from_node_id").Immutable(),
		field.Int("to_node_id").Immutable(),
		field.Enum("relation").Values("leads_to", "compares", "supersedes", "supports", "contradicts", "produced").Immutable(),
	}
}

func (ResearchEdge) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("research_edges").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("research_edges").Field("project_id").Unique().Required().Immutable(),
		edge.From("study", Study.Type).Ref("research_edges").Field("study_id").Unique().Required().Immutable(),
		edge.From("from_node", ResearchNode.Type).Ref("outgoing_edges").Field("from_node_id").Unique().Required().Immutable(),
		edge.From("to_node", ResearchNode.Type).Ref("incoming_edges").Field("to_node_id").Unique().Required().Immutable(),
	}
}

func (ResearchEdge) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("study_id", "from_node_id", "to_node_id", "relation").Unique(),
		index.Fields("study_id", "created_at"),
	}
}
