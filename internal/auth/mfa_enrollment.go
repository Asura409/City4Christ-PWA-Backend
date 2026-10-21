package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"image/png"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

func mfaBinding(account, id string) []byte {
	return []byte("city4christ/mfa-secret/v1:" + account + ":" + id)
}
func (m *MFAService) Begin(ctx context.Context, access string) (MFAEnrollment, error) {
	tx, c, err := m.beginTx(ctx, access)
	if err != nil {
		return MFAEnrollment{}, err
	}
	defer tx.Rollback()
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mfa_factors WHERE account_id=$1 AND confirmed_at IS NOT NULL)`, c.Subject).Scan(&active); err != nil {
		return MFAEnrollment{}, err
	}
	if active {
		return MFAEnrollment{}, ErrMFAConflict
	}
	var email string
	if err = tx.QueryRowContext(ctx, `SELECT email FROM accounts WHERE id=$1`, c.Subject).Scan(&email); err != nil {
		return MFAEnrollment{}, err
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "City4Christ", AccountName: email, Period: 30, SecretSize: 32, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if err != nil {
		return MFAEnrollment{}, err
	}
	image, err := key.Image(256, 256)
	if err != nil {
		return MFAEnrollment{}, err
	}
	var qr bytes.Buffer
	if err = png.Encode(&qr, image); err != nil {
		return MFAEnrollment{}, err
	}
	id := uuid.NewString()
	expires := m.sessions.now().UTC().Add(MFAEnrollmentLifetime)
	sealed := m.cipher.Seal(nil, nil, []byte(key.Secret()), mfaBinding(c.Subject, id))
	if _, err = tx.ExecContext(ctx, `DELETE FROM mfa_factors WHERE account_id=$1`, c.Subject); err != nil {
		return MFAEnrollment{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO mfa_factors(account_id,id,sealed_secret,enrollment_session_id,pending_expires_at) VALUES($1,$2,$3,$4,$5)`, c.Subject, id, sealed, c.SessionID, expires); err != nil {
		return MFAEnrollment{}, err
	}
	if err = m.audit(ctx, tx, c, "enrollment_started"); err != nil {
		return MFAEnrollment{}, err
	}
	if err = tx.Commit(); err != nil {
		return MFAEnrollment{}, err
	}
	return MFAEnrollment{ID: id, Secret: key.Secret(), URI: key.URL(), QRImage: "data:image/png;base64," + base64.StdEncoding.EncodeToString(qr.Bytes()), ExpiresAt: expires}, nil
}

// OTP generation/validation is delegated to pquerna/otp. The persisted counter
// only enforces application-level one-use semantics across concurrent sessions.
func (m *MFAService) matchingCounter(secret, code string, now time.Time, last int64) (int64, error) {
	counter := now.Unix() / 30
	for step := counter - 1; step <= counter+1; step++ {
		if step <= last || step < 0 {
			continue
		}
		valid, err := totp.ValidateCustom(code, secret, time.Unix(step*30, 0), totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err != nil {
			return 0, ErrMFACode
		}
		if valid {
			return step, nil
		}
	}
	return 0, ErrMFACode
}
func (m *MFAService) Confirm(ctx context.Context, access, id, code string) ([]string, error) {
	tx, c, err := m.beginTx(ctx, access)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var sealed []byte
	var expires time.Time
	var sid string
	var confirmed *time.Time
	err = tx.QueryRowContext(ctx, `SELECT sealed_secret,pending_expires_at,enrollment_session_id,confirmed_at FROM mfa_factors WHERE account_id=$1 AND id::text=$2`, c.Subject, id).Scan(&sealed, &expires, &sid, &confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMFACode
	}
	if err != nil {
		return nil, err
	}
	now := m.sessions.now().UTC()
	if sid != c.SessionID || confirmed != nil || !now.Before(expires) {
		return nil, ErrMFACode
	}
	secret, err := m.cipher.Open(nil, nil, sealed, mfaBinding(c.Subject, id))
	if err != nil {
		return nil, err
	}
	step, err := m.matchingCounter(string(secret), code, now, -1)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mfa_factors SET confirmed_at=$2,last_counter=$3 WHERE account_id=$1`, c.Subject, now, step); err != nil {
		return nil, err
	}
	codes, err := m.replaceRecovery(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = m.proof(ctx, tx, c, id); err != nil {
		return nil, err
	}
	if err = m.audit(ctx, tx, c, "enabled"); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}
