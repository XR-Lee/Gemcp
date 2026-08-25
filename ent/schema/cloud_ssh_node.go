package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// CloudSSHNode is an Owner-registered remote cloud instance that Gemcp reaches over SSH.
type CloudSSHNode struct{ ent.Schema }

func (CloudSSHNode) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (CloudSSHNode) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.String("label").NotEmpty().MaxLen(120),
		field.Enum("status").Values("pending_probe", "active", "disabled", "revoked", "host_key_changed").Default("pending_probe"),
		field.String("ssh_host").NotEmpty().MaxLen(255),
		field.Int("ssh_port").Positive().Default(22),
		field.String("ssh_user").NotEmpty().MaxLen(64),
		field.Enum("auth_method").Values("password", "private_key"),
		field.String("credential_ciphertext").Sensitive(),
		field.String("host_key_fingerprint").Optional().MaxLen(128),
		field.JSON("inventory", map[string]any{}).Optional(),
		field.String("created_actor_type").Optional().MaxLen(32),
		field.String("created_actor_id").Optional().MaxLen(128),
		field.Time("last_probed_at").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
	}
}

func (CloudSSHNode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("cloud_ssh_nodes").Field("tenant_id").Unique().Required().Immutable(),
		edge.To("project_access", CloudSSHProjectAccess.Type),
		edge.To("assignments", CloudSSHAssignment.Type),
	}
}

func (CloudSSHNode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "label"),
		index.Fields("tenant_id", "status"),
		index.Fields("tenant_id", "ssh_host", "ssh_port", "ssh_user"),
	}
}
