package schema

import (
	"fmt"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type RecordMixin struct{ ent.Schema }

func enum(values ...string) func(string) error {
	allowed := make(map[string]struct{}, len(values))
	for _, value := range values {
		allowed[value] = struct{}{}
	}
	return func(value string) error {
		if _, ok := allowed[value]; !ok {
			return fmt.Errorf("unsupported value %q", value)
		}
		return nil
	}
}

func (RecordMixin) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("public_id", uuid.UUID{}).Default(uuid.New).Immutable().Unique(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
