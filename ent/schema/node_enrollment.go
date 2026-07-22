package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NodeEnrollment is a short-lived, Owner-issued pairing transaction.
type NodeEnrollment struct{ ent.Schema }

func (NodeEnrollment) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (NodeEnrollment) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.String("label").NotEmpty().MaxLen(120),
		field.Bytes("code_hash").Sensitive().Unique(),
		field.Enum("status").Values("pending", "claimed", "approved", "completed", "revoked").Default("pending"),
		field.Time("expires_at"),
		field.String("pairing_code").Optional().MaxLen(16),
		field.String("installation_id").Optional().MaxLen(120),
		field.String("machine_fingerprint").Optional().MaxLen(128),
		field.JSON("report", map[string]any{}).Optional(),
		field.Time("claimed_at").Optional().Nillable(),
		field.Time("approved_at").Optional().Nillable(),
		field.Time("completed_at").Optional().Nillable(),
		field.Int("node_id").Optional().Nillable(),
	}
}

func (NodeEnrollment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("node_enrollments").Field("tenant_id").Unique().Required().Immutable(),
		edge.To("node", SelfHostedNode.Type).Field("node_id").Unique(),
	}
}

func (NodeEnrollment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "status"),
		index.Fields("expires_at"),
	}
}
