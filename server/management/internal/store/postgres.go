package store

import (
	"context"
	"database/sql"

	"time"

	"github.com/example/runloom/management/internal/recordset"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const postgresSchema = `
CREATE TABLE IF NOT EXISTS runloom_llm_usage (
 id TEXT PRIMARY KEY, ancestors JSONB NOT NULL, status TEXT NOT NULL, body JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS runloom_llm_usage_ancestors_idx ON runloom_llm_usage USING GIN (ancestors);
CREATE INDEX IF NOT EXISTS runloom_llm_usage_started_idx ON runloom_llm_usage ((body->>'started_at'), id);
`

type PostgresStore struct {
	*promptStore
	*projectStore
	db *sql.DB
}

func NewPostgres(ctx context.Context, url string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	return initializePostgres(ctx, db)
}

// Each team uses its own schema. search_path intentionally excludes public.

func NewPostgresTeam(ctx context.Context, url, schema string) (*PostgresStore, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{schema}.Sanitize()); err != nil {
		return nil, err
	}
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	result, err := initializePostgres(ctx, db)
	if err == nil {
		db.SetMaxIdleConns(1)
		db.SetMaxOpenConns(4)
	}
	return result, err
}

func initializePostgres(ctx context.Context, db *sql.DB) (*PostgresStore, error) {
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(20)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, postgresSchema+recordset.Schema); err != nil {
		db.Close()
		return nil, err
	}
	s := &PostgresStore{db: db}
	s.projectStore = newProjectStore(s.readProjects, s.swapProjects, nil)
	s.promptStore = &promptStore{repo: recordset.Postgres(s.db, "prompts")}
	return s, nil
}

func (s *PostgresStore) Driver() string { return "postgres" }

func (s *PostgresStore) Health(ctx context.Context) bool { return s.db.PingContext(ctx) == nil }

func (s *PostgresStore) Close(_ context.Context) error { return s.db.Close() }

type rowScanner interface {
	Scan(...any) error
}
