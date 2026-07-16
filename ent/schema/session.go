package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Session struct{ ent.Schema }

func (Session) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id").Immutable(),
		field.Bytes("token_hash").Sensitive().Unique(),
		field.Bytes("csrf_hash").Sensitive(),
		field.Time("expires_at"),
		field.Time("last_used_at"),
		field.Time("revoked_at").Optional().Nillable(),
	}
}

func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("sessions").Field("user_id").Unique().Required().Immutable(),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at"),
		index.Fields("user_id", "revoked_at"),
	}
}
