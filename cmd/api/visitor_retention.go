package main

import (
	"context"
	"database/sql"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/visitors"
	"log/slog"
	"time"
)

func visitorRetentionPass(ctx context.Context, db *sql.DB, now time.Time) (int, error) {
	total := 0
	for range 10 {
		n, e := visitors.PurgeExpired(ctx, db, now, 100)
		total += n
		if e != nil {
			return total, e
		}
		if n < 100 {
			break
		}
	}
	return total, nil
}

// Consent-approved expiry only scrubs visitor contacts. It never resets a table
// or changes accounts, groups, messages or other staging records.
func runVisitorRetention(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		count, e := visitorRetentionPass(ctx, db, time.Now().UTC())
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			slog.Error("Visitor contact retention pass failed")
		} else if count > 0 {
			slog.Info("Expired visitor contacts removed", "count", count)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
