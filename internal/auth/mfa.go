package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrMFARequired = errors.New("authenticator confirmation required")
	ErrMFACode     = errors.New("invalid or expired authenticator or recovery code")
	ErrMFAConflict = errors.New("authenticator already enabled")
)

const MFAEnrollmentLifetime = 10 * time.Minute
const MFAProofLifetime = 5 * time.Minute

type MFAService struct {
	db       *sql.DB
	sessions *SessionService
	cipher   cipher.AEAD
}
type MFAEnrollment struct {
	ID        string    `json:"id"`
	Secret    string    `json:"secret"`
	URI       string    `json:"uri"`
	QRImage   string    `json:"qrImage"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type MFAStatus struct {
	Enabled                bool       `json:"enabled"`
	Pending                bool       `json:"pending"`
	FreshUntil             *time.Time `json:"freshUntil"`
	RecoveryCodesRemaining int        `json:"recoveryCodesRemaining"`
}

func NewMFAService(db *sql.DB, sessions *SessionService, key []byte) (*MFAService, error) {
	if sessions == nil || len(key) != 32 {
		return nil, ErrVerificationUnavailable
	}
	derived, err := hkdf.Key(sha256.New, key, nil, "city4christ/mfa-secret/v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return &MFAService{db: db, sessions: sessions, cipher: aead}, nil
}

// Serialize every factor mutation on the account before session/factor locks.
// RequireFreshTx checks deadlines after waiting, not just at request arrival.
func (m *MFAService) beginTx(ctx context.Context, access string) (*sql.Tx, *sessionClaims, error) {
	claims, err := m.sessions.parseAccess(access)
	if err != nil {
		return nil, nil, err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND email_verified_at IS NOT NULL FOR UPDATE`, claims.Subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrInvalidSession
	}
	if err == nil {
		err = m.sessions.RequireFreshTx(ctx, tx, access)
	}
	if err != nil {
		tx.Rollback()
		return nil, nil, err
	}
	return tx, claims, nil
}
func (m *MFAService) audit(ctx context.Context, tx *sql.Tx, c *sessionClaims, action string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO mfa_audit_events(account_id,session_id,action,occurred_at) VALUES($1,$2,$3,$4)`, c.Subject, c.SessionID, action, m.sessions.now().UTC())
	return err
}
func (m *MFAService) proof(ctx context.Context, tx *sql.Tx, c *sessionClaims, factor string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO mfa_session_proofs(session_id,factor_id,verified_at) VALUES($1,$2,$3) ON CONFLICT(session_id) DO UPDATE SET factor_id=excluded.factor_id,verified_at=excluded.verified_at`, c.SessionID, factor, m.sessions.now().UTC())
	return err
}
func (m *MFAService) Status(ctx context.Context, access string) (MFAStatus, error) {
	c, err := m.sessions.parseAccess(access)
	if err != nil {
		return MFAStatus{}, err
	}
	var status MFAStatus
	var confirmed, pending, proof, primary *time.Time
	var expiry time.Time
	err = m.db.QueryRowContext(ctx, `SELECT f.confirmed_at,f.pending_expires_at,p.verified_at,COALESCE(s.reauthenticated_at,s.authenticated_at),s.expires_at,(SELECT count(*) FROM mfa_recovery_codes r WHERE r.factor_id=f.id AND r.consumed_at IS NULL) FROM auth_sessions s JOIN accounts a ON a.id=s.account_id LEFT JOIN mfa_factors f ON f.account_id=a.id LEFT JOIN mfa_session_proofs p ON p.session_id=s.id AND p.factor_id=f.id WHERE s.id=$1 AND a.id=$2 AND s.revoked_at IS NULL AND s.expires_at>$3 AND a.email_verified_at IS NOT NULL`, c.SessionID, c.Subject, m.sessions.now().UTC()).Scan(&confirmed, &pending, &proof, &primary, &expiry, &status.RecoveryCodesRemaining)
	if errors.Is(err, sql.ErrNoRows) {
		return status, ErrInvalidSession
	}
	if err != nil {
		return status, err
	}
	now := m.sessions.now().UTC()
	status.Enabled = confirmed != nil
	status.Pending = !status.Enabled && pending != nil && now.Before(*pending)
	if status.Enabled && proof != nil && primary != nil && !proof.After(now) && !primary.After(now) {
		until := minTime(minTime(proof.Add(MFAProofLifetime), primary.Add(ReauthenticationLifetime)), expiry)
		if now.Before(until) {
			status.FreshUntil = &until
		}
	}
	return status, nil
}
