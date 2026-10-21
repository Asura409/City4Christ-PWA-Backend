package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (m *MFAService) Verify(ctx context.Context, access, code string, recovery bool) error {
	tx, c, err := m.beginTx(ctx, access)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	var sealed []byte
	var last int64
	err = tx.QueryRowContext(ctx, `SELECT id,sealed_secret,last_counter FROM mfa_factors WHERE account_id=$1 AND confirmed_at IS NOT NULL`, c.Subject).Scan(&id, &sealed, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMFACode
	}
	if err != nil {
		return err
	}
	action := "totp_verified"
	if recovery {
		if len(code) != 43 {
			return ErrMFACode
		}
		result, err := tx.ExecContext(ctx, `UPDATE mfa_recovery_codes SET consumed_at=$3 WHERE factor_id=$1 AND code_digest=$2 AND consumed_at IS NULL`, id, tokenDigest(code), m.sessions.now().UTC())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrMFACode
		}
		// Lost-device recovery clears previous second-factor proofs. It cannot grant
		// roles, renew primary authentication or disable the factor by itself.
		if _, err = tx.ExecContext(ctx, `DELETE FROM mfa_session_proofs WHERE factor_id=$1`, id); err != nil {
			return err
		}
		action = "recovery_used"
	} else {
		secret, err := m.cipher.Open(nil, nil, sealed, mfaBinding(c.Subject, id))
		if err != nil {
			return err
		}
		step, err := m.matchingCounter(string(secret), code, m.sessions.now().UTC(), last)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE mfa_factors SET last_counter=$2 WHERE account_id=$1`, c.Subject, step); err != nil {
			return err
		}
	}
	if err = m.proof(ctx, tx, c, id); err != nil {
		return err
	}
	if err = m.audit(ctx, tx, c, action); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SessionService) RequireSensitive(ctx context.Context, access string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return s.RequireSensitiveTx(ctx, tx, access)
}
func (s *SessionService) RequireSensitiveTx(ctx context.Context, tx *sql.Tx, access string) error {
	if err := s.RequireFreshTx(ctx, tx, access); err != nil {
		return err
	}
	c, err := s.parseAccess(access)
	if err != nil {
		return err
	}
	return s.requireMFASessionTx(ctx, tx, c.Subject, c.SessionID, false)
}

// requireMFASessionTx requires the caller to hold account/session locks first.
// mandatory=true is reserved for future privileged flows; there are no role
// promotions or implicit provider-MFA assumptions in this implementation.
func (s *SessionService) requireMFASessionTx(ctx context.Context, tx *sql.Tx, account, sid string, mandatory bool) error {
	var at *time.Time
	err := tx.QueryRowContext(ctx, `SELECT p.verified_at FROM mfa_factors f LEFT JOIN mfa_session_proofs p ON p.factor_id=f.id AND p.session_id=$2 WHERE f.account_id=$1 AND f.confirmed_at IS NOT NULL`, account, sid).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		if mandatory {
			return ErrMFARequired
		}
		return nil
	}
	if err != nil {
		return err
	}
	now := s.now().UTC()
	if at == nil || at.After(now) || !now.Before(at.Add(MFAProofLifetime)) {
		return ErrMFARequired
	}
	return nil
}
