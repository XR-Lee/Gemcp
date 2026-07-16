package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type AuditEvent struct{ ent.Schema }

func (AuditEvent) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (AuditEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Enum("actor_type").Values("system", "user", "agent_token").Default("system"),
		field.String("actor_id").Optional().MaxLen(120),
		field.String("action").NotEmpty().MaxLen(160),
		field.String("target_type").NotEmpty().MaxLen(80),
		field.String("target_id").Optional().MaxLen(120),
		field.String("request_id").Optional().MaxLen(120),
		field.JSON("metadata", map[string]any{}).Optional(),
	}
}

func (AuditEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("audit_events").Field("tenant_id").Unique().Required().Immutable(),
	}
}

func (AuditEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "created_at"),
		index.Fields("tenant_id", "action"),
	}
}
