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
		if err := client.Schema.Create(ctx, migrate.WithForeignKeys(true)); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("migrate postgres schema: %w", err)
		}
	}
	return store, nil
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
