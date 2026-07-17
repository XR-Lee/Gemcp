package database

import (
	"testing"

	entmigrate "github.com/XR-Lee/Gemcp/ent/migrate"
)

func TestAttemptResultColumnsRemainNullableForV05Upgrade(t *testing.T) {
	for _, name := range []string{"metrics", "provider_request_ids"} {
		found := false
		for _, column := range entmigrate.AttemptsColumns {
			if column.Name != name {
				continue
			}
			found = true
			if !column.Nullable {
				t.Fatalf("Attempt column %s is non-nullable and cannot migrate historical rows without a database default", name)
			}
		}
		if !found {
			t.Fatalf("Attempt column %s is missing", name)
		}
	}
}
