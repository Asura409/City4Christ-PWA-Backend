package auth

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPersistentRateLimitsAtomicAndExpire(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	key := make([]byte, 32)
	limiter := NewRateLimiter(db, key, func() time.Time { return now })
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			ok, err := limiter.Allow(ctx, "login-account", "test@example.invalid", 5, time.Minute)
			if err != nil {
				t.Error(err)
			}
			if ok {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("concurrent allowed=%d", allowed.Load())
	}
	restarted := NewRateLimiter(db, key, func() time.Time { return now })
	if ok, err := restarted.Allow(ctx, "login-account", "test@example.invalid", 5, time.Minute); ok || err != nil {
		t.Fatalf("restart bypass %v %v", ok, err)
	}
	if ok, err := limiter.Allow(ctx, "login-account", "other@example.invalid", 5, time.Minute); !ok || err != nil {
		t.Fatal("other account blocked")
	}
	now = now.Add(time.Minute)
	if ok, err := limiter.Allow(ctx, "login-account", "test@example.invalid", 5, time.Minute); !ok || err != nil {
		t.Fatal("window failed to expire")
	}
}
