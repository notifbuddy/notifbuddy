// Package store is the Postgres persistence layer for the waitlist service:
// the connection pool, the (single) schema migration, and the one repository
// method. Same shape as the main backend's store, at 1% of the size.
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

// New connects to Postgres using the given URL and verifies the connection.
// Call Migrate after New to ensure the schema exists.
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

// Migrate applies all embedded migration files in lexical order. Migrations
// are idempotent (CREATE TABLE IF NOT EXISTS …), so this runs on every
// startup.
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

// AddToWaitlist records a pre-launch waitlist signup. Idempotent: adding an
// email that is already on the list is a no-op, not an error.
func (s *Store) AddToWaitlist(ctx context.Context, email string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO waitlist (email)
		VALUES ($1)
		ON CONFLICT (email) DO NOTHING
	`, email)
	if err != nil {
		return fmt.Errorf("store: add to waitlist: %w", err)
	}
	return nil
}
