package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SelfHostedNode is an Owner-approved machine that connects to Gemcp over outbound HTTPS.
type SelfHostedNode struct{ ent.Schema }

func (SelfHostedNode) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (SelfHostedNode) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.String("label").NotEmpty().MaxLen(120),
		field.String("token_prefix").NotEmpty().MaxLen(24),
		field.Bytes("token_hash").Sensitive().Unique(),
		field.Enum("status").Values("pending_verification", "active", "draining", "disabled", "verification_required", "revoked").Default("pending_verification"),
		field.Enum("observed_state").Values("unknown", "online", "unavailable", "offline", "lost", "externally_busy", "reconciling", "incompatible").Default("unknown"),
		field.String("installation_id").NotEmpty().MaxLen(120).Immutable(),
		field.String("machine_fingerprint").NotEmpty().MaxLen(128),
		field.String("hostname").NotEmpty().MaxLen(255),
		field.String("operating_system").NotEmpty().MaxLen(120),
		field.String("architecture").NotEmpty().MaxLen(32),
		field.String("agent_version").NotEmpty().MaxLen(64),
		field.String("protocol_version").NotEmpty().MaxLen(32),
		field.JSON("capabilities", map[string]any{}).Optional(),
		field.JSON("storage", map[string]any{}).Optional(),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Time("approved_at").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
	}
}

func (SelfHostedNode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("self_hosted_nodes").Field("tenant_id").Unique().Required().Immutable(),
		edge.To("project_access", NodeProjectAccess.Type),
		edge.To("commands", NodeCommand.Type),
		edge.To("events", NodeEvent.Type),
		edge.To("assignments", NodeAssignment.Type),
		edge.To("workspace_datasets", WorkspaceDataset.Type),
	}
}

func (SelfHostedNode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("token_prefix"),
		index.Fields("tenant_id", "installation_id").Unique(),
		index.Fields("tenant_id", "status", "observed_state"),
		index.Fields("last_seen_at"),
	}
}
