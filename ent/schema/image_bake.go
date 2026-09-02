package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ImageBake struct{ ent.Schema }

func (ImageBake) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ImageBake) Fields() []ent.Field {
	return []ent.Field{
		field.Int("tenant_id").Immutable(),
		field.Int("project_id").Immutable(),
		field.Int("repository_id").Immutable(),
		field.Enum("backend").Values("autodl_pro").Default("autodl_pro").Immutable(),
		field.String("name").NotEmpty().MaxLen(120),
		field.String("base_image_uuid").NotEmpty().MaxLen(512),
		field.String("commit_sha").NotEmpty().MaxLen(64),
		field.String("recipe_path").Default("requirements.gemcp.txt").NotEmpty().MaxLen(256),
		field.Enum("status").Values(
			"requested", "confirmed", "provisioning", "installing", "stopping", "saving",
			"finished", "failed", "cancelled",
		).Default("requested"),
		field.String("confirmation_digest").NotEmpty().MaxLen(80),
		field.String("requested_by").NotEmpty().MaxLen(120).Immutable(),
		field.Enum("requested_by_type").Values("user", "agent_token").Default("agent_token").Immutable(),
		field.String("confirmed_by").Optional().MaxLen(120),
		field.Time("confirmed_at").Optional().Nillable(),
		field.String("image_uuid").Optional().MaxLen(512),
		field.String("instance_uuid").Optional().MaxLen(128),
		field.String("failure_reason").Optional().MaxLen(512),
		field.JSON("proposal", map[string]any{}).Default(map[string]any{}),
		field.Int64("estimated_cost_milli").NonNegative().Default(0),
	}
}

func (ImageBake) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("tenant", Tenant.Type).Ref("image_bakes").Field("tenant_id").Unique().Required().Immutable(),
		edge.From("project", Project.Type).Ref("image_bakes").Field("project_id").Unique().Required().Immutable(),
		edge.From("repository", Repository.Type).Ref("image_bakes").Field("repository_id").Unique().Required().Immutable(),
	}
}

func (ImageBake) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("project_id", "created_at"),
		index.Fields("project_id", "status"),
	}
}
