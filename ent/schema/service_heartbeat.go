package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ServiceHeartbeat struct{ ent.Schema }

func (ServiceHeartbeat) Mixin() []ent.Mixin { return []ent.Mixin{RecordMixin{}} }

func (ServiceHeartbeat) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("role").Values("scheduler", "watchdog", "notification"),
		field.String("instance_id").NotEmpty().MaxLen(255).Immutable(),
		field.Time("last_seen_at").Default(time.Now),
		field.Enum("status").Values("running", "error").Default("running"),
		field.JSON("metadata", map[string]any{}).Default(map[string]any{}),
	}
}

func (ServiceHeartbeat) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("role", "instance_id").Unique(),
		index.Fields("role", "last_seen_at"),
	}
}
