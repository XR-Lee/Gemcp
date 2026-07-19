package database

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	entmigrate "github.com/XR-Lee/Gemcp/ent/migrate"
)

func TestPostgres18ComposeSecuresCorrectedBindRoot(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "deploy", "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	compose := string(content)
	for _, required := range []string{
		"- ${GEMCP_POSTGRES_DATA_DIR:-./postgres_data}:/var/lib/postgresql\n",
		"chown postgres:postgres /var/lib/postgresql",
		"chmod 0700 /var/lib/postgresql",
		"exec docker-entrypoint.sh postgres",
	} {
		if !strings.Contains(compose, required) {
			t.Fatalf("Compose is missing PostgreSQL 18 bind-root protection %q", required)
		}
	}
	if strings.Contains(compose, "./postgres_data:/var/lib/postgresql/data") {
		t.Fatal("Compose uses the PostgreSQL 17 data mount target")
	}
}

func TestAgentEnrollmentSchemaStoresNoRecoverableCredential(t *testing.T) {
	digestFound := false
	for _, column := range entmigrate.AgentEnrollmentsColumns {
		if strings.Contains(column.Name, "ciphertext") || strings.Contains(column.Name, "secret") {
			t.Fatalf("Agent enrollment schema contains recoverable credential column %q", column.Name)
		}
		if column.Name == "code_hash" {
			digestFound = true
			if !column.Unique {
				t.Fatal("Agent enrollment code_hash must be unique")
			}
		}
	}
	if !digestFound {
		t.Fatal("Agent enrollment code_hash column is missing")
	}
}

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
