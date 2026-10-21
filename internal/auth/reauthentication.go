package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const ReauthenticationLifetime = 5 * time.Minute

var ErrReauthenticationRequired = errors.New("fresh reauthentication required")

type ReauthenticationStatus struct {
	Fresh             bool       `json:"fresh"`
	FreshUntil        *time.Time `json:"freshUntil"`
	PasswordAvailable bool       `json:"passwordAvailable"`
	GoogleAvailable   bool       `json:"googleAvailable"`
}

// Freshness belongs to the server-side session, never to a bearer token's iat.
func (s *SessionService) ReauthenticationStatus(ctx context.Context, access string) (ReauthenticationStatus, error) {
	claims, err := s.parseAccess(access)
	if err != nil {
		return ReauthenticationStatus{}, err
	}
	var status ReauthenticationStatus
	var authenticated *time.Time
	var expires time.Time
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(s.reauthenticated_at,s.authenticated_at),s.expires_at,a.password_hash IS NOT NULL,EXISTS(SELECT 1 FROM external_identities e WHERE e.account_id=a.id AND e.provider='google') FROM auth_sessions s JOIN accounts a ON a.id=s.account_id WHERE s.id=$1 AND a.id=$2 AND s.revoked_at IS NULL AND s.expires_at>$3 AND a.email_verified_at IS NOT NULL`, claims.SessionID, claims.Subject, s.now().UTC()).Scan(&authenticated, &expires, &status.PasswordAvailable, &status.GoogleAvailable)
	if errors.Is(err, sql.ErrNoRows) {
		return status, ErrInvalidSession
	}
	if err != nil {
		return status, err
	}
	if authenticated == nil {
		return status, nil
	}
	until := minTime(authenticated.Add(ReauthenticationLifetime), expires)
	now := s.now().UTC()
	status.Fresh = !authenticated.After(now) && now.Before(until)
	if status.Fresh {
		status.FreshUntil = &until
	}
	return status, nil
}

func (s *SessionService) RequireFresh(ctx context.Context, access string) error {
	status, err := s.ReauthenticationStatus(ctx, access)
	if err != nil {
		return err
	}
	if !status.Fresh {
		return ErrReauthenticationRequired
	}
	return nil
}

func (s *SessionService) ReauthenticatePassword(ctx context.Context, access, password string) (ReauthenticationStatus, error) {
	a, err := s.Authenticate(ctx, access)
	if err != nil {
		return ReauthenticationStatus{}, err
	}
	if len(password) > 1024 || a.PasswordHash == nil || !NewPasswordHasher().Verify(password, *a.PasswordHash) {
		return ReauthenticationStatus{}, ErrInvalidCredentials
	}
	claims, err := s.parseAccess(access)
	if err != nil {
		return ReauthenticationStatus{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReauthenticationStatus{}, err
	}
	defer tx.Rollback()
	// Match reset/login lock order; recheck the hash after expensive verification.
	var current *string
	if err = tx.QueryRowContext(ctx, `SELECT password_hash FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL FOR SHARE`, a.ID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return ReauthenticationStatus{}, ErrInvalidSession
	} else if err != nil {
		return ReauthenticationStatus{}, err
	}
	if current == nil || *current != *a.PasswordHash {
		return ReauthenticationStatus{}, ErrInvalidCredentials
	}
	if err = s.recordReauthentication(ctx, tx, claims.Subject, claims.SessionID, "password", s.now().UTC()); err != nil {
		return ReauthenticationStatus{}, err
	}
	if err = tx.Commit(); err != nil {
		return ReauthenticationStatus{}, err
	}
	return s.ReauthenticationStatus(ctx, access)
}

// Caller holds the verified account lock before taking the session lock.
func (s *SessionService) recordReauthentication(ctx context.Context, tx *sql.Tx, account, sid, method string, at time.Time) error {
	var expires time.Time
	var revoked *time.Time
	err := tx.QueryRowContext(ctx, `SELECT expires_at,revoked_at FROM auth_sessions WHERE id=$1 AND account_id=$2 FOR UPDATE`, sid, account).Scan(&expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidSession
	}
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if revoked != nil || !now.Before(expires) {
		return ErrInvalidSession
	}
	if at.After(now) || !now.Before(at.Add(ReauthenticationLifetime)) {
		return ErrReauthenticationRequired
	}
	if _, err = tx.ExecContext(ctx, `UPDATE auth_sessions SET reauthenticated_at=$2,reauthentication_method=$3 WHERE id=$1`, sid, at, method); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO reauthentication_audit_events(account_id,session_id,method,occurred_at) VALUES($1,$2,$3,$4)`, account, sid, method, at)
	return err
}

// RequireFreshTx holds shared account/session locks through the sensitive action
// so logout, reset and refresh replay cannot revoke the proof mid-transaction.
func (s *SessionService) RequireFreshTx(ctx context.Context, tx *sql.Tx, access string) error {
	claims, err := s.parseAccess(access)
	if err != nil {
		return err
	}
	var at *time.Time
	var expires time.Time
	now := s.now().UTC()
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(s.reauthenticated_at,s.authenticated_at),s.expires_at FROM auth_sessions s JOIN accounts a ON a.id=s.account_id WHERE s.id=$1 AND a.id=$2 AND s.revoked_at IS NULL AND s.expires_at>$3 AND a.email_verified_at IS NOT NULL FOR SHARE OF a,s`, claims.SessionID, claims.Subject, now).Scan(&at, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidSession
	}
	if err != nil {
		return err
	}
	now = s.now().UTC()
	if _, err = s.parseAccess(access); err != nil {
		return err
	}
	if !now.Before(expires) {
		return ErrInvalidSession
	}
	if at == nil || at.After(now) || !now.Before(at.Add(ReauthenticationLifetime)) {
		return ErrReauthenticationRequired
	}
	return nil
}
