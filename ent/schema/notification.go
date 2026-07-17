package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Notification struct{ ent.Schema }

func (Notification) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Notification) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.String("dedup_key").NotEmpty().MaxLen(180).Immutable(),
		field.String("kind").NotEmpty().MaxLen(80).Immutable(),
		field.Enum("severity").Values("info", "warning", "critical").Default("critical").Immutable(),
		field.String("subject").NotEmpty().MaxLen(200).Immutable(),
		field.Text("body").Immutable(),
		field.Enum("state").Values("pending", "sending", "sent", "failed").Default("pending"),
		field.Int("attempts").Default(0).NonNegative(),
		field.Time("next_attempt_at").Default(time.Now),
		field.Text("last_error").Optional().Nillable(),
		field.Time("sent_at").Optional().Nillable(),
	}
}

func (Notification) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("notifications").Field("tenant_id").Unique().Required().Immutable(),
	}
}

func (Notification) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "dedup_key").Unique(),
		index.Fields("state", "next_attempt_at", "created_at"),
		index.Fields("tenant_id", "created_at"),
	}
}
