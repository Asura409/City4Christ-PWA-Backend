package auth

import (
	"context"
	"database/sql"
	"errors"
)

var ErrMFAProtected = errors.New("authenticator required by protected church role")

func (m *MFAService) replaceRecovery(ctx context.Context, tx *sql.Tx, id string) ([]string, error) {
	if _, err := tx.ExecContext(ctx, `DELETE FROM mfa_recovery_codes WHERE factor_id=$1`, id); err != nil {
		return nil, err
	}
	codes := make([]string, 10)
	for i := range codes {
		raw, err := randomCredential()
		if err != nil {
			return nil, err
		}
		codes[i] = raw
		if _, err = tx.ExecContext(ctx, `INSERT INTO mfa_recovery_codes(factor_id,code_digest) VALUES($1,$2)`, id, tokenDigest(raw)); err != nil {
			return nil, err
		}
	}
	return codes, nil
}
func (m *MFAService) activeForManagement(ctx context.Context, tx *sql.Tx, c *sessionClaims) (string, error) {
	if err := m.sessions.requireMFASessionTx(ctx, tx, c.Subject, c.SessionID, true); err != nil {
		return "", err
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM mfa_factors WHERE account_id=$1 AND confirmed_at IS NOT NULL`, c.Subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrMFARequired
	}
	return id, err
}
func (m *MFAService) RegenerateRecovery(ctx context.Context, access string) ([]string, error) {
	tx, c, err := m.beginTx(ctx, access)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	id, err := m.activeForManagement(ctx, tx, c)
	if err != nil {
		return nil, err
	}
	codes, err := m.replaceRecovery(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM mfa_session_proofs WHERE factor_id=$1`, id); err != nil {
		return nil, err
	}
	if err = m.proof(ctx, tx, c, id); err != nil {
		return nil, err
	}
	if err = m.audit(ctx, tx, c, "recovery_regenerated"); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}
func (m *MFAService) Disable(ctx context.Context, access string) error {
	tx, c, err := m.beginTx(ctx, access)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := m.activeForManagement(ctx, tx, c)
	if err != nil {
		return err
	}
	// beginTx holds the account UPDATE lock. Role grants hold a target account
	// SHARE lock, so a concurrent grant and factor removal cannot both succeed.
	var protected bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_roles WHERE account_id=$1 AND role IN ('super_administrator','assistant_pastor','staff','finance_officer'))`, c.Subject).Scan(&protected); err != nil {
		return err
	}
	if protected {
		return ErrMFAProtected
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM mfa_factors WHERE id=$1`, id); err != nil {
		return err
	}
	if err = m.audit(ctx, tx, c, "disabled"); err != nil {
		return err
	}
	return tx.Commit()
}
