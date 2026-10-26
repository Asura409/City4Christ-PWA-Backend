package messaging

import (
	"context"
	"database/sql"
	"time"
)

func (r *Repository) issueRealtimeTicket(ctx context.Context, digest []byte, accountID, sessionID string, accessExpiresAt, now, expiresAt time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM realtime_tickets WHERE expires_at <= $1 OR consumed_at IS NOT NULL`, now); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO realtime_tickets(token_hash,account_id,expires_at,session_id,access_expires_at)
SELECT $1,$2,$3,$4,$5 WHERE EXISTS(
 SELECT 1 FROM auth_sessions s JOIN accounts a ON a.id=s.account_id
 WHERE s.id=$4 AND s.account_id=$2 AND s.revoked_at IS NULL AND s.expires_at>$6
 AND a.email_verified_at IS NOT NULL
)`, digest, accountID, expiresAt, sessionID, accessExpiresAt, now)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (r *Repository) consumeRealtimeTicket(ctx context.Context, digest []byte, now time.Time) (RealtimeTicketIdentity, error) {
	var identity RealtimeTicketIdentity
	err := r.db.QueryRowContext(ctx, `UPDATE realtime_tickets t SET consumed_at=$2
WHERE t.token_hash=$1 AND t.consumed_at IS NULL AND t.expires_at>$2 AND t.access_expires_at>$2
AND EXISTS (
 SELECT 1 FROM auth_sessions s JOIN accounts a ON a.id=s.account_id
 WHERE s.id=t.session_id AND s.account_id=t.account_id AND s.revoked_at IS NULL
 AND s.expires_at>$2 AND a.email_verified_at IS NOT NULL
)
RETURNING t.account_id::text,t.session_id::text,t.access_expires_at`, digest, now).Scan(&identity.AccountID, &identity.SessionID, &identity.AccessExpiresAt)
	if err == sql.ErrNoRows {
		return RealtimeTicketIdentity{}, ErrNotFound
	}
	identity.AccessExpiresAt = identity.AccessExpiresAt.UTC()
	return identity, err
}
