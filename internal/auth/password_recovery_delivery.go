package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"
)

// DeliverNext uses the same account-first lock order as issuance and redemption.
// Provider idempotency makes retries safe after ambiguous network failures.
func (s *PasswordRecoveryService) DeliverNext(ctx context.Context, sender RecoveryEmailSender) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	now := s.verification.now().UTC()
	var accountID string
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM accounts a JOIN password_recovery_outbox o ON o.account_id=a.id WHERE o.expires_at<=$1 OR (o.failed_at IS NULL AND o.next_attempt_at<=$1) ORDER BY o.created_at,o.id LIMIT 1 FOR UPDATE OF a SKIP LOCKED`, now).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var id, kind string
	var sealed []byte
	var expiry time.Time
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT id,kind,sealed_payload,expires_at,delivery_attempts FROM password_recovery_outbox WHERE account_id=$1 AND (expires_at<=$2 OR (failed_at IS NULL AND next_attempt_at<=$2)) ORDER BY created_at,id LIMIT 1`, accountID, now).Scan(&id, &kind, &sealed, &expiry, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	discard := func() (bool, error) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM password_recovery_outbox WHERE id=$1", id); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	if !now.Before(expiry) {
		return discard()
	}
	a, err := scanAccount(tx.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM accounts WHERE id=$1", accountID))
	if err != nil {
		return true, err
	}
	m, openErr := s.OpenEmail(accountID, id, sealed)
	var failure error
	if openErr != nil || m.Email != a.Email || !m.ExpiresAt.Equal(expiry) || m.PasswordChanged != (kind == "changed") {
		failure = &EmailDeliveryError{Code: "outbox_unreadable"}
	}
	if failure == nil && kind == "reset" {
		u, parseErr := url.Parse(m.ResetURL)
		if parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || !strings.HasPrefix(u.Fragment, "reset_token=") {
			failure = &EmailDeliveryError{Code: "outbox_unreadable"}
		} else {
			token := strings.TrimPrefix(u.Fragment, "reset_token=")
			var valid bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM password_recoveries WHERE account_id=$1 AND challenge_id=$2 AND token_digest=$3 AND consumed_at IS NULL AND expires_at>$4)`, accountID, id, tokenDigest(token), now).Scan(&valid)
			if err != nil {
				return true, err
			}
			if !valid || a.PasswordHash == nil || a.EmailVerifiedAt == nil {
				return discard()
			}
		}
	}
	if failure == nil {
		remaining := expiry.Sub(s.verification.now().UTC())
		if remaining <= 0 {
			return discard()
		}
		sendCtx, cancel := context.WithTimeout(ctx, min(10*time.Second, remaining))
		failure = sender.SendRecovery(sendCtx, id, m)
		cancel()
	}
	if failure == nil {
		return discard()
	}
	classified := safeDeliveryFailure(failure)
	attempts++
	now = s.verification.now().UTC()
	delay := 5 * time.Second * time.Duration(1<<min(attempts-1, 4))
	if classified.RetryAfter > delay {
		delay = min(classified.RetryAfter, 24*time.Hour)
	}
	var failedAt *time.Time
	if !classified.Retryable || attempts >= 5 {
		failedAt = &now
	}
	if _, err = tx.ExecContext(ctx, `UPDATE password_recovery_outbox SET delivery_attempts=$2,next_attempt_at=$3,failed_at=$4,last_error_code=$5 WHERE id=$1`, id, attempts, now.Add(delay), failedAt, classified.Code); err != nil {
		return true, err
	}
	if err = tx.Commit(); err != nil {
		return true, err
	}
	return true, classified
}
