package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type IdempotencyRecord struct{ ent.Schema }

func (IdempotencyRecord) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (IdempotencyRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("agent_token_id").Immutable(),
		field.Int("experiment_id").Immutable(),
		field.Bytes("key_hash").Immutable().Sensitive(),
		field.Bytes("request_fingerprint").Immutable().Sensitive(),
		field.Time("expires_at").Immutable(),
	}
}

func (IdempotencyRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("idempotency_records").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("agent_token", AgentToken.Type).Ref("idempotency_records").Field("agent_token_id").Unique().Required().Immutable(),
		edge.From("experiment", Experiment.Type).Ref("idempotency_records").Field("experiment_id").Unique().Required().Immutable(),
	}
}

func (IdempotencyRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("agent_token_id", "key_hash").Unique(),
		index.Fields("expires_at"),
	}
}
