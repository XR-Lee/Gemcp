package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type AgentToken struct{ ent.Schema }

func (AgentToken) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (AgentToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id").Immutable(),
		field.String("label").NotEmpty().MaxLen(120),
		field.String("prefix").NotEmpty().MaxLen(24),
		field.Bytes("token_hash").Sensitive().Unique(),
		field.Enum("principal_type").Values("agent", "shared").Default("agent"),
		field.Strings("scopes").Default([]string{"submit", "read", "cancel"}),
		field.Enum("status").Values("active", "revoked").Default("active"),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("last_used_at").Optional().Nillable(),
	}
}

func (AgentToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("agent_tokens").Field("project_id").Unique().Required().Immutable(),
		edge.To("experiments", Experiment.Type),
		edge.To("idempotency_records", IdempotencyRecord.Type),
		edge.To("experiment_proposals", ExperimentProposal.Type),
		edge.To("workspace_datasets", WorkspaceDataset.Type),
	}
}

func (AgentToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("prefix"),
		index.Fields("project_id", "status"),
	}
}
