package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type NotificationSetting struct{ ent.Schema }

func (NotificationSetting) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (NotificationSetting) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Bool("enabled").Default(false),
		field.String("host").NotEmpty().MaxLen(255),
		field.Int("port").Positive(),
		field.Enum("tls_mode").Values("starttls", "tls").Default("starttls"),
		field.String("username").Optional().MaxLen(320),
		field.String("password_ciphertext").Optional().Sensitive(),
		field.String("from_address").NotEmpty().MaxLen(320),
		field.Strings("recipients"),
		field.Enum("status").Values("ready", "error").Default("ready"),
		field.Time("last_tested_at").Optional().Nillable(),
		field.Text("last_error").Optional().Nillable(),
	}
}

func (NotificationSetting) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("notification_settings").Field("tenant_id").Unique().Required().Immutable(),
	}
}

func (NotificationSetting) Indexes() []ent.Index {
	return []ent.Index{index.Fields("tenant_id").Unique()}
}
