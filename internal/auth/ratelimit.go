package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"time"
)

type RateLimiter struct {
	db  *sql.DB
	key []byte
	now func() time.Time
}

func NewRateLimiter(db *sql.DB, key []byte, now func() time.Time) *RateLimiter {
	if now == nil {
		now = time.Now
	}
	return &RateLimiter{db: db, key: append([]byte(nil), key...), now: now}
}
func (l *RateLimiter) Allow(ctx context.Context, scope, identity string, limit int, window time.Duration) (bool, error) {
	mac := hmac.New(sha256.New, l.key)
	_, _ = mac.Write([]byte("city4christ/rate/v1:" + scope + ":" + identity))
	key := mac.Sum(nil)
	now := l.now().UTC()
	var attempts int
	err := l.db.QueryRowContext(ctx, `INSERT INTO auth_rate_limits(bucket,attempts,expires_at) VALUES($1,1,$2)
 ON CONFLICT(bucket) DO UPDATE SET attempts=CASE WHEN auth_rate_limits.expires_at<=$3 THEN 1 ELSE LEAST(auth_rate_limits.attempts+1,$4+1) END,
 expires_at=CASE WHEN auth_rate_limits.expires_at<=$3 THEN $2 ELSE auth_rate_limits.expires_at END RETURNING attempts`, key, now.Add(window), now, limit).Scan(&attempts)
	return err == nil && attempts <= limit, err
}
