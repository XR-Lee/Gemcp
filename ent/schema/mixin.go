package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type RecordMixin struct{ ent.Schema }

func (RecordMixin) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("public_id", uuid.UUID{}).Default(uuid.New).Immutable().Unique(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
