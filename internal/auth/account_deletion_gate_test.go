package auth

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/google/uuid"
)

func TestAccountDeletionGatesExistingCredentialsAndRecovery(t *testing.T) {
	db, change, sessions, account, tokens := passwordChangeFixture(t)
	ctx := context.Background()
	recovery := change.recovery
	if err := recovery.Request(ctx, account.Email, "https://frontend.example.invalid/auth.html"); err != nil {
		t.Fatal(err)
	}
	var id string
	var sealed []byte
	if err := db.QueryRow("SELECT id,sealed_payload FROM password_recovery_outbox WHERE account_id=$1 AND kind='reset'", account.ID).Scan(&id, &sealed); err != nil {
		t.Fatal(err)
	}
	message, err := recovery.OpenEmail(account.ID, id, sealed)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(message.ResetURL)
	if err != nil {
		t.Fatal(err)
	}
	reset := strings.TrimPrefix(u.Fragment, "reset_token=")
	if len(reset) != 43 {
		t.Fatal("invalid fixture reset")
	}
	provider := &fakeGoogle{identity: GoogleIdentity{Subject: "deletion-test-subject", Email: account.Email, EmailVerified: true}}
	google := NewGoogleService(db, sessions, provider, true, nil)
	if _, err = db.Exec("INSERT INTO external_identities(provider,subject,account_id) VALUES('google',$1,$2)", provider.identity.Subject, account.ID); err != nil {
		t.Fatal(err)
	}
	flow, err := google.Begin(ctx, "deletion-browser", "")
	if err != nil {
		t.Fatal(err)
	}
	flowURL, _ := url.Parse(flow.URL)
	if _, err = db.Exec("UPDATE accounts SET deletion_requested_at=now() WHERE id=$1", account.ID); err != nil {
		t.Fatal(err)
	}
	t.Run("bearer and new sessions", func(t *testing.T) {
		if _, err := sessions.Authenticate(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
			t.Fatal("pending deletion authenticated", err)
		}
		if active, err := sessions.SessionActive(ctx, account.ID, mustSessionID(t, sessions, tokens.AccessToken)); err != nil || active {
			t.Fatal("pending deletion session active", active, err)
		}
		if _, err := sessions.Start(ctx, account.ID); !errors.Is(err, ErrInvalidSession) {
			t.Fatal("trusted start reactivated pending account", err)
		}
		if _, err := sessions.Login(ctx, account.Email, "original long password"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal("password login reactivated pending account", err)
		}
		if _, err := sessions.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidSession) {
			t.Fatal("refresh reactivated pending account", err)
		}
	})
	t.Run("Google callback", func(t *testing.T) {
		if _, err := google.Complete(ctx, flowURL.Query().Get("state"), "deletion-browser", "valid"); !errors.Is(err, ErrInvalidSession) {
			t.Fatal("Google reactivated pending account", err)
		}
	})
	t.Run("security proofs and password mutation", func(t *testing.T) {
		if err := sessions.RequireFresh(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
			t.Fatal("fresh proof accepted", err)
		}
		mfa, err := NewMFAService(db, sessions, make([]byte, 32))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = mfa.Status(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
			t.Fatal("MFA status accepted", err)
		}
		if _, err = mfa.Begin(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
			t.Fatal("MFA enrollment accepted", err)
		}
		if err = change.Change(ctx, tokens.AccessToken, "original long password", "replacement long password"); !errors.Is(err, ErrInvalidSession) {
			t.Fatal("password change accepted", err)
		}
		if err = recovery.Reset(ctx, reset, "replacement long password"); !errors.Is(err, ErrInvalidResetToken) {
			t.Fatal("reset accepted", err)
		}
	})
	t.Run("recovery worker suppresses queued mail", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		now := recovery.verification.now().UTC().Truncate(time.Microsecond)
		if err = recovery.queue(ctx, tx, account, uuid.NewString(), "changed", RecoveryEmail{Email: account.Email, PasswordChanged: true, ExpiresAt: now.Add(time.Hour)}, now); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			_, err = recovery.DeliverNext(ctx, recoverySenderFunc(func(context.Context, string, RecoveryEmail) error {
				t.Fatal("mail delivered after deletion request")
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
		}
		if err = recovery.Request(ctx, account.Email, "https://frontend.example.invalid/auth.html"); err != nil {
			t.Fatal("generic recovery request leaked state", err)
		}
		var count int
		if err = db.QueryRow("SELECT count(*) FROM password_recovery_outbox WHERE account_id=$1", account.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("recovery mail requeued", count, err)
		}
	})
}

func mustSessionID(t *testing.T, s *SessionService, access string) string {
	t.Helper()
	claims, err := s.parseAccess(access)
	if err != nil {
		t.Fatal(err)
	}
	return claims.SessionID
}

func TestAccountDeletionGatesVerificationAndDelivery(t *testing.T) {
	db, change, _, _, _ := passwordChangeFixture(t)
	ctx := context.Background()
	v := change.recovery.verification
	account, err := v.register(ctx, NewAccount{Email: "deletion-unverified@example.invalid", FirstName: "Synthetic", LastName: "Unverified"})
	if err != nil {
		t.Fatal(err)
	}
	message, id, _ := queuedVerification(t, db, v, account.ID)
	if _, err = db.Exec("UPDATE accounts SET deletion_requested_at=now() WHERE id=$1", account.ID); err != nil {
		t.Fatal(err)
	}
	if err = v.Verify(ctx, account.Email, message.Code); !errors.Is(err, ErrInvalidOTP) {
		t.Fatal("OTP reverified pending account", err)
	}
	if err = v.Resend(ctx, account.Email); err != nil {
		t.Fatal("resend disclosed pending account", err)
	}
	var existing string
	if err = db.QueryRow("SELECT challenge_id FROM verification_email_outbox WHERE account_id=$1", account.ID).Scan(&existing); err != nil || existing != id {
		t.Fatal("resend replaced challenge", err)
	}
	processed, err := v.DeliverNext(ctx, emailSenderFunc(func(context.Context, string, VerificationEmail) error {
		t.Fatal("OTP delivered after deletion request")
		return nil
	}))
	if err != nil || !processed {
		t.Fatal("pending OTP not discarded", processed, err)
	}
	var verified bool
	if err = db.QueryRow("SELECT email_verified_at IS NOT NULL FROM accounts WHERE id=$1", account.ID).Scan(&verified); err != nil || verified {
		t.Fatal("pending account verified", verified, err)
	}
}

func TestAccountDeletionSerializesRefresh(t *testing.T) {
	// Refresh must also serialize correctly for Google/passwordless sessions;
	// this fixture needs no unrelated password hashing work.
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	account, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "deletion-refresh@example.invalid", FirstName: "Synthetic", LastName: "Refresh"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET email_verified_at=now() WHERE id=$1", account.ID); err != nil {
		t.Fatal(err)
	}
	sessions, err := NewSessionService(db, SessionConfig{Key: bytes.Repeat([]byte{73}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Start(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if _, err = holder.Exec("SELECT id FROM accounts WHERE id=$1 FOR UPDATE", account.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := sessions.Refresh(ctx, tokens.RefreshToken); done <- err }()
	deadline := time.Now().Add(30 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatal("refresh bypassed account lock", err)
		default:
		}
		var blocked bool
		if err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock')").Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("refresh did not wait for account lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = holder.Exec("UPDATE accounts SET deletion_requested_at=now() WHERE id=$1", account.ID); err != nil {
		t.Fatal(err)
	}
	if err = holder.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, ErrInvalidSession) {
		t.Fatal("blocked refresh resurrected pending account", err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM refresh_tokens").Scan(&count); err != nil || count != 1 {
		t.Fatal("refresh issued a replacement", count, err)
	}
}
