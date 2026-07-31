package lock_test

import (
	"context"
	"testing"

	"xolo/backend/internal/lock"
)

func TestNopRuns(t *testing.T) {
	t.Parallel()
	called := false
	err := lock.Nop{}.WithLock(context.Background(), "k", func(context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("Nop: called=%v err=%v", called, err)
	}
}
