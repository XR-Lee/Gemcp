package database

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
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

func TestNodeCredentialSchemasStoreOnlyDigests(t *testing.T) {
	check := func(table string, columns []*schema.Column) {
		t.Helper()
		digestFound := false
		for _, column := range columns {
			if strings.Contains(column.Name, "ciphertext") || strings.Contains(column.Name, "secret") || column.Name == "token" || column.Name == "code" {
				t.Fatalf("%s contains recoverable credential column %q", table, column.Name)
			}
			if column.Name == "code_hash" || column.Name == "token_hash" {
				digestFound = true
				if !column.Unique {
					t.Fatalf("%s digest column %s must be unique", table, column.Name)
				}
			}
		}
		if !digestFound {
			t.Fatalf("%s has no credential digest column", table)
		}
	}
	check("node_enrollments", entmigrate.NodeEnrollmentsColumns)
	check("self_hosted_nodes", entmigrate.SelfHostedNodesColumns)
}

func TestCloudSSHSchemasKeepCiphertextOnTheNodeAndAssignmentsCredentialFree(t *testing.T) {
	ciphertextFound := false
	for _, column := range entmigrate.CloudSSHNodesColumns {
		if column.Name == "credential_ciphertext" {
			ciphertextFound = true
		}
		name := strings.ToLower(column.Name)
		if name == "password" || name == "private_key" || name == "passphrase" {
			t.Fatalf("cloud_ssh_nodes stores recoverable plaintext column %q", column.Name)
		}
	}
	if !ciphertextFound {
		t.Fatal("cloud_ssh_nodes.credential_ciphertext is missing")
	}
	for _, column := range entmigrate.CloudSSHAssignmentsColumns {
		name := strings.ToLower(column.Name)
		if strings.Contains(name, "token") || strings.Contains(name, "credential") || strings.Contains(name, "ciphertext") || strings.Contains(name, "secret") || strings.Contains(name, "password") || strings.Contains(name, "private_key") {
			t.Fatalf("cloud_ssh_assignments contains credential-like column %q", column.Name)
		}
	}
	for _, column := range entmigrate.CloudSSHProjectAccessesColumns {
		name := strings.ToLower(column.Name)
		if strings.Contains(name, "token") || strings.Contains(name, "credential") || strings.Contains(name, "ciphertext") || strings.Contains(name, "secret") {
			t.Fatalf("cloud_ssh_project_accesses contains credential-like column %q", column.Name)
		}
	}
}

func TestNodeAssignmentSchemaContainsNoCredentialMaterial(t *testing.T) {
	outputRefFound := false
	for _, column := range entmigrate.NodeAssignmentsColumns {
		name := strings.ToLower(column.Name)
		if strings.Contains(name, "token") || strings.Contains(name, "credential") || strings.Contains(name, "ciphertext") || strings.Contains(name, "secret") {
			t.Fatalf("node_assignments contains credential-like column %q", column.Name)
		}
		if column.Name == "output_ref" {
			outputRefFound = true
		}
	}
	if !outputRefFound {
		t.Fatal("node_assignments output_ref column is missing")
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

func TestBudgetAdjustmentsCanBeProjectScopedAndIdempotent(t *testing.T) {
	columns := map[string]*schema.Column{}
	for _, column := range entmigrate.BudgetEntriesColumns {
		columns[column.Name] = column
	}
	for _, name := range []string{"experiment_id", "idempotency_key"} {
		column := columns[name]
		if column == nil {
			t.Fatalf("budget_entries column %s is missing", name)
		}
		if !column.Nullable {
			t.Fatalf("budget_entries column %s must be nullable for historical and Project-level entries", name)
		}
	}
	foundUnique := false
	for _, index := range entmigrate.BudgetEntriesTable.Indexes {
		if index.Name == "budgetentry_project_id_idempotency_key" {
			foundUnique = index.Unique
		}
	}
	if !foundUnique {
		t.Fatal("budget adjustment Project/idempotency key unique index is missing")
	}
}

func TestOwnerDiagnosticsUseNullableAgentAttributionAndHashedIdempotency(t *testing.T) {
	agentTokenNullable := false
	for _, column := range entmigrate.ExperimentsColumns {
		if column.Name == "agent_token_id" {
			agentTokenNullable = column.Nullable
			break
		}
	}
	if !agentTokenNullable {
		t.Fatal("Experiment agent_token_id must be nullable for Owner-attributed diagnostic runs")
	}
	columns := map[string]*schema.Column{}
	for _, column := range entmigrate.DiagnosticRunsColumns {
		columns[column.Name] = column
		name := strings.ToLower(column.Name)
		if name == "token" || name == "credential" || name == "idempotency_key" || strings.Contains(name, "ciphertext") || strings.Contains(name, "secret") {
			t.Fatalf("diagnostic_runs contains recoverable credential-like column %q", column.Name)
		}
	}
	for _, required := range []string{"experiment_id", "backend", "suite", "requested_by", "idempotency_key_hash", "request_fingerprint", "preflight"} {
		if columns[required] == nil {
			t.Fatalf("diagnostic_runs column %s is missing", required)
		}
	}
	foundUnique := false
	for _, index := range entmigrate.DiagnosticRunsTable.Indexes {
		if index.Name == "diagnosticrun_project_id_idempotency_key_hash" {
			foundUnique = index.Unique
		}
	}
	if !foundUnique {
		t.Fatal("diagnostic Project/idempotency digest unique index is missing")
	}
}

func TestPreparedExperimentSchemaIsCredentialFreeAndExecutionMigrationIsAdditive(t *testing.T) {
	proposalColumns := map[string]*schema.Column{}
	for _, column := range entmigrate.ExperimentProposalsColumns {
		proposalColumns[column.Name] = column
		name := strings.ToLower(column.Name)
		if name == "token" || name == "credential" || strings.Contains(name, "ciphertext") || strings.Contains(name, "secret") || strings.Contains(name, "private_key") {
			t.Fatalf("experiment_proposals contains recoverable credential-like column %q", column.Name)
		}
	}
	for _, required := range []string{
		"project_id", "agent_token_id", "repository_id", "environment_id", "resource_profile_id", "status",
		"requested_ref", "commit_sha", "execution_mode", "argv", "display_command", "checks",
		"reserved_cost_milli", "confirmation_digest", "expires_at", "experiment_id",
	} {
		if proposalColumns[required] == nil {
			t.Fatalf("experiment_proposals column %s is missing", required)
		}
	}
	if proposalColumns["agent_token_id"] == nil || !proposalColumns["agent_token_id"].Nullable {
		t.Fatal("experiment_proposals agent_token_id must be nullable for Owner-authored prepare")
	}
	experimentColumns := map[string]*schema.Column{}
	for _, column := range entmigrate.ExperimentsColumns {
		experimentColumns[column.Name] = column
	}
	executionMode := experimentColumns["execution_mode"]
	if executionMode == nil || executionMode.Nullable || executionMode.Default == nil {
		t.Fatal("experiments execution_mode must have a non-null shell migration default")
	}
	argv := experimentColumns["argv"]
	if argv == nil || !argv.Nullable {
		t.Fatal("experiments argv must be nullable so historical shell rows migrate without a JSON database default")
	}
}
