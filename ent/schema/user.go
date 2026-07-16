package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type User struct{ ent.Schema }

func (User) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.String("email").NotEmpty().MaxLen(320),
		field.String("password_hash").Sensitive(),
		field.Enum("role").Values("owner", "admin", "operator", "viewer").Default("owner"),
		field.Enum("status").Values("active", "disabled").Default("active"),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("users").Field("tenant_id").Unique().Required().Immutable(),
		edge.To("sessions", Session.Type),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{index.Fields("tenant_id", "email").Unique()}
}
