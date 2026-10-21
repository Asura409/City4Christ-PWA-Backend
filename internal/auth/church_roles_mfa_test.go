package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPrivilegedGuardRequiresConfirmedSessionMFA(t *testing.T) {
	db, s, m, tokens, now := mfaFixture(t)
	ctx := context.Background()
	guard := func(token string) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		return s.RequirePrivilegedTx(ctx, tx, token)
	}
	if err := guard(tokens.AccessToken); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("unenrolled: %v", err)
	}
	setup, err := m.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if err = guard(tokens.AccessToken); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("pending: %v", err)
	}
	if _, err = m.Confirm(ctx, tokens.AccessToken, setup.ID, otpCode(t, setup.Secret, *now)); err != nil {
		t.Fatal(err)
	}
	if err = guard(tokens.AccessToken); err != nil {
		t.Fatal(err)
	}
	account, err := s.Authenticate(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.Start(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = guard(other.AccessToken); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("other session: %v", err)
	}
	if _, err = db.Exec(`UPDATE mfa_session_proofs SET verified_at=$1`, now.Add(-MFAProofLifetime)); err != nil {
		t.Fatal(err)
	}
	if err = guard(tokens.AccessToken); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("expired MFA: %v", err)
	}
	*now = now.Add(ReauthenticationLifetime + time.Second)
	if err = guard(tokens.AccessToken); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatalf("expired primary: %v", err)
	}

	if err = guard("invalid-token"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("invalid bearer: %v", err)
	}
	if _, err = db.Exec(`UPDATE auth_sessions SET revoked_at=$1`, *now); err != nil {
		t.Fatal(err)
	}
	if err = guard(tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("revoked session: %v", err)
	}
}

func TestChurchRolePreventsMFADisableAtomically(t *testing.T) {
	for _, role := range []string{"super_administrator", "assistant_pastor", "staff", "finance_officer"} {
		t.Run(role, func(t *testing.T) {
			db, s, m, tokens, now := mfaFixture(t)
			ctx := context.Background()
			setup, err := m.Begin(ctx, tokens.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = m.Confirm(ctx, tokens.AccessToken, setup.ID, otpCode(t, setup.Secret, *now)); err != nil {
				t.Fatal(err)
			}
			a, err := s.Authenticate(ctx, tokens.AccessToken)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`INSERT INTO account_roles(account_id,role) VALUES($1,$2)`, a.ID, role); err != nil {
				t.Fatal(err)
			}
			if err = m.Disable(ctx, tokens.AccessToken); !errors.Is(err, ErrMFAProtected) {
				t.Fatalf("protected disable: %v", err)
			}
			var factors, codes, proofs, audits int
			if err = db.QueryRow(`SELECT (SELECT count(*) FROM mfa_factors),(SELECT count(*) FROM mfa_recovery_codes),(SELECT count(*) FROM mfa_session_proofs),(SELECT count(*) FROM mfa_audit_events WHERE action='disabled')`).Scan(&factors, &codes, &proofs, &audits); err != nil {
				t.Fatal(err)
			}
			if factors != 1 || codes != 10 || proofs != 1 || audits != 0 {
				t.Fatalf("partial protected disable: %d %d %d %d", factors, codes, proofs, audits)
			}
			if _, err = db.Exec(`DELETE FROM account_roles WHERE account_id=$1`, a.ID); err != nil {
				t.Fatal(err)
			}
			if err = m.Disable(ctx, tokens.AccessToken); err != nil {
				t.Fatal("role-free disable", err)
			}
		})
	}
}
