package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/XR-Lee/Gemcp/ent"
	"github.com/XR-Lee/Gemcp/ent/migrate"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const schemaMigrationLockID int64 = 0x47656d63704d6967

type Store struct {
	DB     *sql.DB
	Client *ent.Client
}

func Open(ctx context.Context, databaseURL string, autoMigrate bool) (*Store, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := ent.NewClient(ent.Driver(driver))
	store := &Store{DB: db, Client: client}
	if autoMigrate {
		if err := migrateSchema(ctx, db, client); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("migrate postgres schema: %w", err)
		}
	}
	return store, nil
}

func migrateSchema(ctx context.Context, db *sql.DB, client *ent.Client) error {
	lockConnection, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve schema migration lock connection: %w", err)
	}
	defer lockConnection.Close()
	if _, err := lockConnection.ExecContext(ctx, "SELECT pg_advisory_lock($1)", schemaMigrationLockID); err != nil {
		return fmt.Errorf("acquire schema migration lock: %w", err)
	}
	defer func() {
		releaseContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = lockConnection.ExecContext(releaseContext, "SELECT pg_advisory_unlock($1)", schemaMigrationLockID)
	}()
	return client.Schema.Create(ctx, migrate.WithForeignKeys(true))
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("postgres store is not initialized")
	}
	return s.DB.PingContext(ctx)
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	if s.Client != nil {
		return s.Client.Close()
	}
	if s.DB != nil {
		return s.DB.Close()
	}
	return nil
}
