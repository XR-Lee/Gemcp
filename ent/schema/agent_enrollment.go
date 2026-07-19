package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type AgentEnrollment struct{ ent.Schema }

func (AgentEnrollment) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (AgentEnrollment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id").Immutable(),
		field.String("label").NotEmpty().MaxLen(120),
		field.Bytes("code_hash").Sensitive().Unique(),
		field.Strings("scopes"),
		field.Enum("status").Values("pending", "claimed", "completed", "revoked").Default("pending"),
		field.Time("expires_at"),
		field.Int("token_expires_in_days").Positive().Optional().Nillable(),
		field.Time("claimed_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Int("agent_token_id").Optional().Nillable(),
		field.JSON("verification", map[string]any{}).Optional(),
	}
}

func (AgentEnrollment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("agent_enrollments").Field("project_id").Unique().Required().Immutable(),
		edge.To("agent_token", AgentToken.Type).Field("agent_token_id").Unique(),
	}
}

func (AgentEnrollment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "status"),
		index.Fields("expires_at"),
	}
}
