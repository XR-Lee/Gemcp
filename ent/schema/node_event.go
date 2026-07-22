package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NodeEvent is an idempotent observed-state event from a node outbox.
type NodeEvent struct{ ent.Schema }

func (NodeEvent) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (NodeEvent) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("node_id").Immutable(),
		field.String("event_id").NotEmpty().MaxLen(120).Immutable(),
		field.Int64("sequence").Positive().Immutable(),
		field.String("kind").NotEmpty().MaxLen(80).Immutable(),
		field.JSON("payload", map[string]any{}).Optional().Immutable(),
		field.Time("occurred_at").Immutable(),
	}
}

func (NodeEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("node_events").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("node", SelfHostedNode.Type).Ref("events").Field("node_id").Unique().Required().Immutable(),
	}
}

func (NodeEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("node_id", "event_id").Unique(),
		index.Fields("node_id", "sequence").Unique(),
		index.Fields("node_id", "created_at"),
	}
}
