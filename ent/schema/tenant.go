package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Tenant struct{ ent.Schema }

func (Tenant) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Tenant) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().MaxLen(120),
		field.String("status").Default("active").MaxLen(32),
	}
}

func (Tenant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("users", User.Type),
		edge.To("provider_accounts", ProviderAccount.Type),
		edge.To("projects", Project.Type),
		edge.To("audit_events", AuditEvent.Type),
	}
}
