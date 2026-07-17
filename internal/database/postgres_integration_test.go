package database

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPostgresMigration(t *testing.T) {
	databaseURL := os.Getenv("GEMCP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GEMCP_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := Open(ctx, databaseURL, true)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
	if _, err := store.Client.Experiment.Query().Count(ctx); err != nil {
		t.Fatalf("query migrated experiments table: %v", err)
	}
	if _, err := store.Client.BudgetEntry.Query().Count(ctx); err != nil {
		t.Fatalf("query migrated budget entries table: %v", err)
	}
	tenant, err := store.Client.Tenant.Create().SetName("migration-test").Save(ctx)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := store.Client.Tenant.DeleteOneID(tenant.ID).Exec(ctx); err != nil {
		t.Fatalf("delete tenant: %v", err)
	}
}
