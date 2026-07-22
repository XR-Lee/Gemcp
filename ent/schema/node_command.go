package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NodeCommand is durable desired state delivered at least once to one node.
type NodeCommand struct{ ent.Schema }

func (NodeCommand) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (NodeCommand) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("node_id").Immutable(),
		field.Int("assignment_id").Optional().Nillable().Immutable(),
		field.Int64("sequence").Positive().Immutable(),
		field.String("kind").NotEmpty().MaxLen(80).Immutable(),
		field.String("idempotency_key").NotEmpty().MaxLen(160).Immutable(),
		field.JSON("payload", map[string]any{}).Optional().Immutable(),
		field.Enum("status").Values("pending", "delivered", "acknowledged", "completed", "failed", "cancelled").Default("pending"),
		field.Time("available_at"),
		field.Time("delivered_at").Optional().Nillable(),
		field.Time("acknowledged_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.JSON("result", map[string]any{}).Optional(),
		field.Text("last_error").Optional().Nillable(),
	}
}

func (NodeCommand) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("node_commands").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("node", SelfHostedNode.Type).Ref("commands").Field("node_id").Unique().Required().Immutable(),
		edge.From("assignment", NodeAssignment.Type).Ref("commands").Field("assignment_id").Unique().Immutable(),
	}
}

func (NodeCommand) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("node_id", "sequence").Unique(),
		index.Fields("node_id", "idempotency_key").Unique(),
		index.Fields("node_id", "status", "available_at"),
	}
}
