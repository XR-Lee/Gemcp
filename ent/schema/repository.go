package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Repository struct{ ent.Schema }

func (Repository) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (Repository) Fields() []ent.Field {
	return []ent.Field{
		field.Int("project_id").Immutable(),
		field.String("name").NotEmpty().MaxLen(120),
		field.String("ssh_url").NotEmpty().MaxLen(512),
		field.String("ssh_host").NotEmpty().MaxLen(255),
		field.String("host_key_fingerprint").Optional().MaxLen(255),
		field.String("deploy_public_key").Optional().Sensitive(),
		field.String("deploy_private_key_ciphertext").Optional().Sensitive(),
		field.Enum("status").Values("pending_key", "active", "disabled", "error").Default("pending_key"),
		field.Time("last_verified_at").Optional().Nillable(),
	}
}

func (Repository) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("project", Project.Type).Ref("repositories").Field("project_id").Unique().Required().Immutable(),
	}
}

func (Repository) Indexes() []ent.Index {
	return []ent.Index{index.Fields("project_id", "name").Unique()}
}
