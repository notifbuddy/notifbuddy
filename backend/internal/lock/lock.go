package lock

import "context"

type Locker interface {
	WithLock(ctx context.Context, key string, fn func(context.Context) error) error
}

type Nop struct{}

func (Nop) WithLock(ctx context.Context, _ string, fn func(context.Context) error) error {
	return fn(ctx)
}
