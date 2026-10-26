package messaging

import (
	"context"
	"database/sql"
	"time"
)

func (r *Repository) issueRealtimeTicket(ctx context.Context, digest []byte, accountID string, now, expiresAt time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM realtime_tickets WHERE expires_at <= $1 OR consumed_at IS NOT NULL`, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO realtime_tickets(token_hash,account_id,expires_at) VALUES($1,$2,$3)`, digest, accountID, expiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) consumeRealtimeTicket(ctx context.Context, digest []byte, now time.Time) (string, error) {
	var accountID string
	err := r.db.QueryRowContext(ctx, `UPDATE realtime_tickets SET consumed_at=$2 WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at > $2 RETURNING account_id`, digest, now).Scan(&accountID)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return accountID, err
}
