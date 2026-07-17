package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ProviderResource is the durable ownership boundary for a resource created by Gemcp.
type ProviderResource struct{ ent.Schema }

func (ProviderResource) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ProviderResource) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("experiment_id").Immutable(),
		field.Int("attempt_id").Immutable(),
		field.Int("provider_account_id").Immutable(),
		field.Enum("kind").Values("deployment").Default("deployment").Immutable(),
		field.String("name").NotEmpty().MaxLen(120).Immutable(),
		field.String("provider_id").Optional().Nillable().MaxLen(255),
		field.Enum("state").Values("creating", "active", "stopping", "stopped", "deleting", "deleted", "error").Default("creating"),
		field.Bool("owned").Default(true).Immutable(),
		field.String("provider_status").Optional().Nillable().MaxLen(120),
		field.Int("create_attempts").Default(0).NonNegative(),
		field.Time("create_attempted_at").Optional().Nillable(),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Time("terminal_observed_at").Optional().Nillable(),
		field.Time("hard_deadline_at").Optional().Nillable(),
		field.Time("stop_requested_at").Optional().Nillable(),
		field.String("stop_reason").Optional().Nillable().MaxLen(80),
		field.Time("stopped_at").Optional().Nillable(),
		field.Time("delete_requested_at").Optional().Nillable(),
		field.Time("deleted_at").Optional().Nillable(),
		field.Int64("price_milli_per_hour").Default(0).NonNegative(),
		field.Time("provider_started_at").Optional().Nillable(),
		field.JSON("request_ids", map[string]string{}).Default(map[string]string{}),
		field.Text("last_error").Optional().Nillable(),
		field.String("lease_owner").Optional().Nillable().MaxLen(255),
		field.Time("lease_expires_at").Optional().Nillable(),
	}
}

func (ProviderResource) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("provider_resources").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("provider_resources").Field("project_id").Unique().Required().Immutable(),
		edge.From("experiment", Experiment.Type).Ref("provider_resources").Field("experiment_id").Unique().Required().Immutable(),
		edge.From("attempt", Attempt.Type).Ref("owned_resource").Field("attempt_id").Unique().Required().Immutable(),
		edge.From("provider_account", ProviderAccount.Type).Ref("provider_resources").Field("provider_account_id").Unique().Required().Immutable(),
	}
}

func (ProviderResource) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_id").Unique(),
		index.Fields("state", "hard_deadline_at"),
		index.Fields("state", "lease_expires_at"),
		index.Fields("experiment_id", "state"),
	}
}
