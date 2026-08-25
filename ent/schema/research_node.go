package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ResearchNode is one visible claim, run, or decision in a Study Graph.
type ResearchNode struct{ ent.Schema }

func (ResearchNode) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ResearchNode) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("study_id").Immutable(),
		field.Int("experiment_id").Optional().Nillable(),
		field.Int("agent_token_id").Optional().Nillable().Immutable(),
		field.Enum("kind").Values("question", "hypothesis", "plan", "run", "result", "observation", "decision").Immutable(),
		field.String("title").NotEmpty().MaxLen(160),
		field.Text("summary").Optional(),
		field.Enum("status").Values("open", "running", "succeeded", "failed", "superseded").Default("open"),
		field.String("metric_name").Optional().Nillable().MaxLen(80),
		field.Float("metric_value").Optional().Nillable(),
		field.Time("occurred_at").Optional().Nillable().Comment("scientific time: git committer date or Experiment time, not MCP write time"),
		field.String("commit_sha").Optional().Nillable().MaxLen(64).Comment("optional evidence commit; Graph stays claim-based"),
	}
}

func (ResearchNode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("research_nodes").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("research_nodes").Field("project_id").Unique().Required().Immutable(),
		edge.From("study", Study.Type).Ref("research_nodes").Field("study_id").Unique().Required().Immutable(),
		edge.From("experiment", Experiment.Type).Ref("research_nodes").Field("experiment_id").Unique(),
		edge.From("agent_token", AgentToken.Type).Ref("research_nodes").Field("agent_token_id").Unique().Immutable(),
		edge.To("outgoing_edges", ResearchEdge.Type),
		edge.To("incoming_edges", ResearchEdge.Type),
	}
}

func (ResearchNode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("study_id", "created_at"),
		index.Fields("study_id", "occurred_at"),
		index.Fields("study_id", "kind", "status"),
		index.Fields("experiment_id").Unique(),
		index.Fields("project_id", "updated_at"),
	}
}
