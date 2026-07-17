package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ProviderAccount struct{ ent.Schema }

func (ProviderAccount) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ProviderAccount) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.String("name").NotEmpty().MaxLen(120),
		field.String("base_url").NotEmpty().MaxLen(512),
		field.Enum("backend").Values("unverified", "elastic", "private", "pro").Default("unverified"),
		field.String("credential_ciphertext").Sensitive(),
		field.Enum("status").Values("pending_validation", "active", "disabled", "error").Default("pending_validation"),
		field.Time("last_validated_at").Optional().Nillable(),
	}
}

func (ProviderAccount) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("provider_accounts").Field("tenant_id").Unique().Required().Immutable(),
	}
}

func (ProviderAccount) Indexes() []ent.Index {
	return []ent.Index{index.Fields("tenant_id", "name").Unique()}
}
