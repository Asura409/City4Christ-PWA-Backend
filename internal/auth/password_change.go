package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrCurrentPasswordInvalid = errors.New("current password is incorrect")
	ErrLocalPasswordRequired  = errors.New("account has no local password")
)

type PasswordChangeService struct {
	db       *sql.DB
	sessions *SessionService
	recovery *PasswordRecoveryService
}

func NewPasswordChangeService(db *sql.DB, sessions *SessionService, recovery *PasswordRecoveryService) *PasswordChangeService {
	return &PasswordChangeService{db: db, sessions: sessions, recovery: recovery}
}

// Change requires the current local password and an active verified session.
// Password work happens before locking; the account hash and exact session are
// then checked again under locks shared with reset/login and session revocation.
func (s *PasswordChangeService) Change(ctx context.Context, access, currentPassword, newPassword string) error {
	if s.sessions == nil {
		return ErrInvalidSession
	}
	a, err := s.sessions.Authenticate(ctx, access)
	if err != nil {
		return err
	}
	if a.PasswordHash == nil {
		return ErrLocalPasswordRequired
	}
	if !utf8.ValidString(newPassword) || utf8.RuneCountInString(newPassword) < 15 || len(newPassword) > 1024 || newPassword == currentPassword {
		return &ValidationError{Fields: map[string]string{"newPassword": "Use a different password with at least 15 characters and at most 1024 bytes."}}
	}
	if len(currentPassword) > 1024 || !utf8.ValidString(currentPassword) || !NewPasswordHasher().Verify(currentPassword, *a.PasswordHash) {
		return ErrCurrentPasswordInvalid
	}
	hash, err := NewPasswordHasher().Hash(newPassword)
	if err != nil {
		return err
	}
	claims, err := s.sessions.parseAccess(access)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	locked, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=$1 FOR UPDATE`, a.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidSession
	}
	if err != nil {
		return err
	}
	if locked.EmailVerifiedAt == nil || locked.DeletionRequestedAt != nil {
		return ErrInvalidSession
	}
	if locked.PasswordHash == nil {
		return ErrLocalPasswordRequired
	}
	if *locked.PasswordHash != *a.PasswordHash {
		return ErrCurrentPasswordInvalid
	}
	var expires time.Time
	var revoked *time.Time
	err = tx.QueryRowContext(ctx, `SELECT expires_at,revoked_at FROM auth_sessions WHERE id=$1 AND account_id=$2 FOR UPDATE`, claims.SessionID, a.ID).Scan(&expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidSession
	}
	if err != nil {
		return err
	}
	now := s.recovery.verification.now().UTC().Truncate(time.Microsecond)
	if revoked != nil || !now.Before(expires) {
		return ErrInvalidSession
	}
	if _, err = s.sessions.parseAccess(access); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET password_hash=$2 WHERE id=$1`, a.ID, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE password_recoveries SET consumed_at=$2,token_digest=NULL WHERE account_id=$1`, a.ID, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,$2) WHERE account_id=$1`, a.ID, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM password_recovery_outbox WHERE account_id=$1 AND kind='reset'`, a.ID); err != nil {
		return err
	}
	if err = s.recovery.queue(ctx, tx, locked, uuid.NewString(), "changed", RecoveryEmail{Email: locked.Email, PasswordChanged: true, ExpiresAt: now.Add(24 * time.Hour)}, now); err != nil {
		return err
	}
	return tx.Commit()
}
