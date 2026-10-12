package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DeliverNext claims one account, revalidates its current OTP, and dispatches
// at most one email. All auth operations lock the account before touching the
// outbox, so a resend cannot replace an email while it is being dispatched.
func (s *VerificationService) DeliverNext(ctx context.Context, sender VerificationEmailSender) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var accountID string
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM accounts a
 JOIN verification_email_outbox o ON o.account_id=a.id
 LEFT JOIN email_verifications c ON c.account_id=a.id AND c.challenge_id=o.challenge_id
 WHERE c.account_id IS NULL OR a.email_verified_at IS NOT NULL OR c.consumed_at IS NOT NULL
 OR c.attempts>=5 OR c.expires_at<=$1 OR (o.failed_at IS NULL AND o.next_attempt_at<=$1)
 ORDER BY o.created_at,o.account_id LIMIT 1 FOR UPDATE OF a SKIP LOCKED`, s.now().UTC()).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	account, err := scanAccount(tx.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM accounts WHERE id=$1", accountID))
	if err != nil {
		return true, err
	}
	var challengeID string
	var sealed []byte
	var attempts int
	if err := tx.QueryRowContext(ctx, "SELECT challenge_id,sealed_payload,delivery_attempts FROM verification_email_outbox WHERE account_id=$1", accountID).Scan(&challengeID, &sealed, &attempts); err != nil {
		return true, err
	}
	challenge, err := loadChallenge(ctx, tx, accountID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return true, err
	}
	now := s.now().UTC()
	discard := func() (bool, error) {
		if _, err := tx.ExecContext(ctx, "DELETE FROM verification_email_outbox WHERE account_id=$1", accountID); err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	if errors.Is(err, sql.ErrNoRows) || account.EmailVerifiedAt != nil || challenge.id != challengeID || challenge.consumedAt != nil || challenge.attempts >= VerificationMaxAttempts || !now.Before(challenge.expiresAt) {
		return discard()
	}
	message, openErr := s.OpenEmail(accountID, challengeID, sealed)
	var sendErr error
	if openErr != nil || message.Email != account.Email || !s.matches(accountID, challengeID, message.Code, challenge.digest) {
		sendErr = &EmailDeliveryError{Code: "outbox_unreadable"}
	} else {
		remaining := challenge.expiresAt.Sub(s.now().UTC())
		if remaining <= 0 {
			return discard()
		}
		timeout := min(10*time.Second, remaining)
		sendCtx, cancel := context.WithTimeout(ctx, timeout)
		sendErr = sender.Send(sendCtx, challengeID, message)
		cancel()
	}
	if sendErr == nil {
		return discard()
	}
	now = s.now().UTC()
	failure := safeDeliveryFailure(sendErr)
	attempts++
	delay := 5 * time.Second * time.Duration(1<<min(attempts-1, 4))
	if failure.RetryAfter > delay {
		delay = min(failure.RetryAfter, 24*time.Hour)
	}
	var failedAt *time.Time
	if !failure.Retryable || attempts >= 5 {
		failedAt = &now
	}
	if _, err := tx.ExecContext(ctx, `UPDATE verification_email_outbox SET delivery_attempts=$2,next_attempt_at=$3,
 failed_at=$4,last_error_code=$5 WHERE account_id=$1`, accountID, attempts, now.Add(delay), failedAt, failure.Code); err != nil {
		return true, err
	}
	if err := tx.Commit(); err != nil {
		return true, err
	}
	return true, failure
}

func safeDeliveryFailure(err error) *EmailDeliveryError {
	var failure *EmailDeliveryError
	if errors.As(err, &failure) {
		switch failure.Code {
		case "provider_rate_limited", "provider_unavailable", "provider_rejected", "invalid_email_payload", "outbox_unreadable":
			copy := *failure
			return &copy
		}
	}
	return &EmailDeliveryError{Code: "provider_unavailable", Retryable: true}
}
