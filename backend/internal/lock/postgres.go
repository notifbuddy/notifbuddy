package lock

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) WithLock(ctx context.Context, key string, fn func(context.Context) error) error {
	if p == nil || p.pool == nil {
		return fn(ctx)
	}
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("lock: acquire: %w", err)
	}
	defer conn.Release()

	k1, k2 := hashKey(key)
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1, $2)`, k1, k2); err != nil {
		return fmt.Errorf("lock: advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1, $2)`, k1, k2)
	}()

	return fn(ctx)
}

func hashKey(key string) (int32, int32) {
	h1 := fnv.New32a()
	_, _ = h1.Write([]byte(key))
	h2 := fnv.New32a()
	_, _ = h2.Write([]byte("lock:" + key))
	return int32(h1.Sum32()), int32(h2.Sum32())
}
