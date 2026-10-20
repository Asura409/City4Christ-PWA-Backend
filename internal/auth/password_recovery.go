package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const PasswordResetLifetime = 15 * time.Minute

var ErrInvalidResetToken = errors.New("invalid or expired password reset token")

type RecoveryEmail struct {
	Email           string    `json:"email"`
	ResetURL        string    `json:"resetUrl,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt"`
	PasswordChanged bool      `json:"passwordChanged,omitempty"`
}
type RecoveryEmailSender interface {
	SendRecovery(context.Context, string, RecoveryEmail) error
}
type PasswordRecoveryService struct {
	db           *sql.DB
	verification *VerificationService
}

func NewPasswordRecoveryService(db *sql.DB, v *VerificationService) *PasswordRecoveryService {
	return &PasswordRecoveryService{db: db, verification: v}
}
func recoveryBinding(accountID, id string) []byte {
	return []byte("password-recovery:v1:" + accountID + ":" + id)
}
func (s *PasswordRecoveryService) OpenEmail(accountID, id string, sealed []byte) (RecoveryEmail, error) {
	raw, err := s.verification.emailCipher.Open(nil, nil, sealed, recoveryBinding(accountID, id))
	if err != nil {
		return RecoveryEmail{}, err
	}
	var message RecoveryEmail
	err = json.Unmarshal(raw, &message)
	return message, err
}
func (s *PasswordRecoveryService) queue(ctx context.Context, tx *sql.Tx, a Account, id, kind string, message RecoveryEmail, now time.Time) error {
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	sealed := s.verification.emailCipher.Seal(nil, nil, raw, recoveryBinding(a.ID, id))
	_, err = tx.ExecContext(ctx, `INSERT INTO password_recovery_outbox(id,account_id,kind,sealed_payload,expires_at,created_at,next_attempt_at) VALUES($1,$2,$3,$4,$5,$6,$6)`, id, a.ID, kind, sealed, message.ExpiresAt, now)
	return err
}

// Request never discloses eligibility or cooldown state. The HTTP adapter must
// validate returnTo against configured frontend origins, never the Host header.
func (s *PasswordRecoveryService) Request(ctx context.Context, email, returnTo string) error {
	raw, err := randomCredential()
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := lockedAccount(ctx, tx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if a.PasswordHash == nil || a.EmailVerifiedAt == nil {
		return nil
	}
	// PostgreSQL timestamps have microsecond precision; bind the encrypted
	// message to the exact timestamp that is persisted.
	now := s.verification.now().UTC().Truncate(time.Microsecond)
	var last time.Time
	err = tx.QueryRowContext(ctx, "SELECT last_sent_at FROM password_recoveries WHERE account_id=$1", a.ID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && now.Before(last.Add(time.Minute)) {
		return nil
	}
	id := uuid.NewString()
	expiry := now.Add(PasswordResetLifetime)
	if _, err = tx.ExecContext(ctx, "DELETE FROM password_recovery_outbox WHERE account_id=$1 AND kind='reset'", a.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO password_recoveries(account_id,challenge_id,token_digest,expires_at,last_sent_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(account_id) DO UPDATE SET challenge_id=EXCLUDED.challenge_id,token_digest=EXCLUDED.token_digest,expires_at=EXCLUDED.expires_at,last_sent_at=EXCLUDED.last_sent_at,consumed_at=NULL`, a.ID, id, tokenDigest(raw), expiry, now); err != nil {
		return err
	}
	u, err := url.Parse(returnTo)
	if err != nil {
		return err
	}
	u.Fragment = "reset_token=" + raw
	if err = s.queue(ctx, tx, a, id, "reset", RecoveryEmail{Email: a.Email, ResetURL: u.String(), ExpiresAt: expiry}, now); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *PasswordRecoveryService) Reset(ctx context.Context, token, password string) error {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 15 || len(password) > 1024 {
		return &ValidationError{Fields: map[string]string{"password": "Use at least 15 characters and at most 1024 bytes."}}
	}
	if len(token) != 43 {
		return ErrInvalidResetToken
	}
	// Reject random/expired tokens before spending Argon2id work. This read is
	// only a preflight: redemption still rechecks under the account lock.
	var eligible bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM password_recoveries WHERE token_digest=$1 AND consumed_at IS NULL AND expires_at>$2)`, tokenDigest(token), s.verification.now().UTC()).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return ErrInvalidResetToken
	}
	// Hash before locking the account; recheck token under the lock afterwards.
	hash, err := NewPasswordHasher().Hash(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	a, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+prefixedAccountColumns+` FROM accounts a JOIN password_recoveries p ON p.account_id=a.id WHERE p.token_digest=$1 FOR UPDATE OF a`, tokenDigest(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidResetToken
	}
	if err != nil {
		return err
	}
	// PostgreSQL timestamps have microsecond precision; bind the encrypted
	// message to the exact timestamp that is persisted.
	now := s.verification.now().UTC().Truncate(time.Microsecond)
	var id string
	err = tx.QueryRowContext(ctx, `SELECT challenge_id FROM password_recoveries WHERE account_id=$1 AND token_digest=$2 AND consumed_at IS NULL AND expires_at>$3`, a.ID, tokenDigest(token), now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) || a.PasswordHash == nil || a.EmailVerifiedAt == nil {
		return ErrInvalidResetToken
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE accounts SET password_hash=$2 WHERE id=$1", a.ID, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE password_recoveries SET consumed_at=$2,token_digest=NULL WHERE account_id=$1", a.ID, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE account_id=$1", a.ID, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM password_recovery_outbox WHERE account_id=$1 AND kind='reset'", a.ID); err != nil {
		return err
	}
	if err = s.queue(ctx, tx, a, uuid.NewString(), "changed", RecoveryEmail{Email: a.Email, PasswordChanged: true, ExpiresAt: now.Add(24 * time.Hour)}, now); err != nil {
		return err
	}
	return tx.Commit()
}
