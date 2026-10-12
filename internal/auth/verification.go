package auth

import (
	"context"
	"crypto/cipher"
	"database/sql"
	"errors"
	"time"
)

const (
	VerificationLifetime       = 10 * time.Minute
	VerificationResendCooldown = time.Minute
	VerificationMaxAttempts    = 5
)

var (
	ErrInvalidOTP              = errors.New("invalid or expired verification code")
	ErrResendCooldown          = errors.New("verification resend cooldown")
	ErrVerificationUnavailable = errors.New("verification is not configured")
)

type VerificationConfig struct {
	// Key is a securely generated 32-byte server secret, never a password.
	Key []byte
	Now func() time.Time
}

type VerificationEmail struct {
	Email     string    `json:"email"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type VerificationService struct {
	db          *sql.DB
	digestKey   []byte
	emailCipher cipher.AEAD
	now         func() time.Time
}

type verificationChallenge struct {
	id         string
	digest     []byte
	expiresAt  time.Time
	lastSentAt time.Time
	attempts   int
	consumedAt *time.Time
}

func (s *VerificationService) register(ctx context.Context, input NewAccount) (Account, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	account, err := createAccount(ctx, tx, input)
	if err != nil {
		return Account{}, err
	}
	if err := s.issue(ctx, tx, account, nil, s.now().UTC()); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return Account{}, err
	}
	return account, nil
}

func lockedAccount(ctx context.Context, tx *sql.Tx, email string) (Account, error) {
	return scanAccount(tx.QueryRowContext(ctx, "SELECT "+accountColumns+" FROM accounts WHERE email=$1 FOR UPDATE", normalizeEmail(email)))
}

func loadChallenge(ctx context.Context, tx *sql.Tx, accountID string) (verificationChallenge, error) {
	var c verificationChallenge
	err := tx.QueryRowContext(ctx, "SELECT challenge_id,code_digest,expires_at,last_sent_at,attempts,consumed_at FROM email_verifications WHERE account_id=$1", accountID).Scan(&c.id, &c.digest, &c.expiresAt, &c.lastSentAt, &c.attempts, &c.consumedAt)
	return c, err
}

// Verify serializes all attempts for an account, including concurrent resends.
// Failed guesses are committed before returning the generic invalid-code error.
func (s *VerificationService) Verify(ctx context.Context, email, code string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	account, err := lockedAccount(ctx, tx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidOTP
	}
	if err != nil {
		return err
	}
	if account.EmailVerifiedAt != nil {
		return ErrInvalidOTP
	}
	c, err := loadChallenge(ctx, tx, account.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidOTP
	}
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if c.consumedAt != nil || !now.Before(c.expiresAt) || c.attempts >= VerificationMaxAttempts {
		return ErrInvalidOTP
	}
	if !s.matches(account.ID, c.id, code, c.digest) {
		if _, err := tx.ExecContext(ctx, "UPDATE email_verifications SET attempts=attempts+1 WHERE account_id=$1", account.ID); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return ErrInvalidOTP
	}
	if _, err := tx.ExecContext(ctx, "UPDATE email_verifications SET attempts=attempts+1,consumed_at=$2,code_digest=NULL WHERE account_id=$1", account.ID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE accounts SET email_verified_at=$2 WHERE id=$1", account.ID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM verification_email_outbox WHERE account_id=$1", account.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *VerificationService) Resend(ctx context.Context, email string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	account, err := lockedAccount(ctx, tx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if account.EmailVerifiedAt != nil {
		return nil
	}
	c, err := loadChallenge(ctx, tx, account.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := s.now().UTC()
	var previous *verificationChallenge
	if err == nil {
		if now.Before(c.lastSentAt.Add(VerificationResendCooldown)) {
			return ErrResendCooldown
		}
		previous = &c
	}
	if err := s.issue(ctx, tx, account, previous, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *VerificationService) issue(ctx context.Context, tx *sql.Tx, account Account, previous *verificationChallenge, now time.Time) error {
	issued, err := s.prepareEmail(account, previous, now)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM verification_email_outbox WHERE account_id=$1", account.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO email_verifications(account_id,challenge_id,code_digest,expires_at,last_sent_at)
 VALUES ($1,$2,$3,$4,$5) ON CONFLICT(account_id) DO UPDATE SET challenge_id=EXCLUDED.challenge_id,
 code_digest=EXCLUDED.code_digest,expires_at=EXCLUDED.expires_at,last_sent_at=EXCLUDED.last_sent_at,attempts=0,consumed_at=NULL`, account.ID, issued.id, issued.digest, issued.expiresAt, now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO verification_email_outbox(account_id,challenge_id,sealed_payload,created_at,next_attempt_at) VALUES ($1,$2,$3,$4,$4)", account.ID, issued.id, issued.sealed, now)
	return err
}
