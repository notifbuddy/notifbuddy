// Package store is the Postgres persistence layer for xolo. It owns the
// connection pool, schema migrations, and the repository methods the rest of
// the app uses. The integration token columns are written already-encrypted by
// the caller (see internal/crypto); store never sees plaintext tokens.
package store

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps a pgx connection pool plus the repository methods.
type Store struct {
	pool *pgxpool.Pool
}

// New connects to Postgres using the given URL (e.g.
// postgres://user@localhost:5432/xolo?sslmode=disable) and verifies the
// connection. Call Migrate after New to ensure the schema exists.
func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// Migrate applies all embedded migration files in lexical order. Migrations are
// expected to be idempotent (CREATE TABLE IF NOT EXISTS …), so this is safe to
// run on every startup — simple and sufficient for this app's needs.
func (s *Store) Migrate(ctx context.Context) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", name, err)
		}
		if _, err := s.pool.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("store: apply migration %s: %w", name, err)
		}
	}
	return nil
}
