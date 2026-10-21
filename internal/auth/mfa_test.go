package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
	"github.com/pquerna/otp/totp"
)

func mfaFixture(t *testing.T) (*sql.DB, *SessionService, *MFAService, SessionTokens, *time.Time) {
	t.Helper()
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	key := []byte(strings.Repeat("m", 32))
	s, _ := NewSessionService(db, SessionConfig{Key: key, Now: func() time.Time { return now }})
	var id string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('mfa@example.invalid','MFA','Tester',$1) RETURNING id`, now).Scan(&id); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.Start(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMFAService(db, s, key)
	if err != nil {
		t.Fatal(err)
	}
	return db, s, m, tokens, &now
}
func otpCode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestMFAEnrollmentAndSessionBoundStepUp(t *testing.T) {
	db, s, m, tokens, now := mfaFixture(t)
	ctx := context.Background()
	enrollment, err := m.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if enrollment.Secret == "" || !strings.HasPrefix(enrollment.URI, "otpauth://totp/") || enrollment.ID == "" {
		t.Fatal("missing private setup data")
	}
	var sealed []byte
	if err = db.QueryRow(`SELECT sealed_secret FROM mfa_factors`).Scan(&sealed); err != nil || strings.Contains(string(sealed), enrollment.Secret) {
		t.Fatal("unencrypted secret", err)
	}
	a, err := s.Authenticate(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	code := otpCode(t, enrollment.Secret, *now)
	if _, err = m.Confirm(ctx, second.AccessToken, enrollment.ID, code); !errors.Is(err, ErrMFACode) {
		t.Fatal("other session activated enrollment", err)
	}
	codes, err := m.Confirm(ctx, tokens.AccessToken, enrollment.ID, code)
	if err != nil || len(codes) != 10 {
		t.Fatal("activation", len(codes), err)
	}
	if err = s.RequireSensitive(ctx, tokens.AccessToken); err != nil {
		t.Fatal("activation not verified", err)
	}
	if err = s.RequireSensitive(ctx, second.AccessToken); !errors.Is(err, ErrMFARequired) {
		t.Fatal("proof leaked between sessions", err)
	}
	if _, err = m.Begin(ctx, tokens.AccessToken); !errors.Is(err, ErrMFAConflict) {
		t.Fatal("active factor replaced", err)
	}
	if err = m.Verify(ctx, second.AccessToken, code, false); !errors.Is(err, ErrMFACode) {
		t.Fatal("activation OTP replay", err)
	}
	*now = now.Add(30 * time.Second)
	if err = m.Verify(ctx, second.AccessToken, otpCode(t, enrollment.Secret, *now), false); err != nil {
		t.Fatal(err)
	}
	if err = s.RequireSensitive(ctx, second.AccessToken); err != nil {
		t.Fatal(err)
	}
	if err = m.Verify(ctx, second.AccessToken, codes[0], true); err != nil {
		t.Fatal("recovery", err)
	}
	if err = m.Verify(ctx, second.AccessToken, codes[0], true); !errors.Is(err, ErrMFACode) {
		t.Fatal("recovery replay", err)
	}
	status, err := m.Status(ctx, second.AccessToken)
	if err != nil || !status.Enabled || status.RecoveryCodesRemaining != 9 || status.FreshUntil == nil {
		t.Fatal(status, err)
	}
	var digest []byte
	if err = db.QueryRow(`SELECT code_digest FROM mfa_recovery_codes LIMIT 1`).Scan(&digest); err != nil || len(digest) != 32 {
		t.Fatal("recovery digest", err)
	}
	*now = now.Add(5 * time.Minute)
	refreshed, err := s.Refresh(ctx, second.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RequireSensitive(ctx, refreshed.AccessToken); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("refresh extended primary freshness", err)
	}
	claims, err := s.parseAccess(refreshed.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE auth_sessions SET reauthenticated_at=$2,reauthentication_method='google' WHERE id=$1`, claims.SessionID, *now); err != nil {
		t.Fatal(err)
	}
	if err = s.RequireSensitive(ctx, refreshed.AccessToken); !errors.Is(err, ErrMFARequired) {
		t.Fatal("primary confirmation renewed expired MFA", err)
	}
}

func TestMFAPendingExpiryAuditRollbackAndRecoveryRotation(t *testing.T) {
	db, s, m, tokens, now := mfaFixture(t)
	ctx := context.Background()
	first, err := m.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Confirm(ctx, tokens.AccessToken, first.ID, otpCode(t, first.Secret, *now)); !errors.Is(err, ErrMFACode) {
		t.Fatal("replaced enrollment active", err)
	}
	if _, err = db.Exec(`ALTER TABLE mfa_audit_events RENAME TO unavailable_mfa_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Confirm(ctx, tokens.AccessToken, second.ID, otpCode(t, second.Secret, *now)); err == nil {
		t.Fatal("activation without audit")
	}
	if _, err = db.Exec(`ALTER TABLE unavailable_mfa_audit RENAME TO mfa_audit_events`); err != nil {
		t.Fatal(err)
	}
	codes, err := m.Confirm(ctx, tokens.AccessToken, second.ID, otpCode(t, second.Secret, *now))
	if err != nil {
		t.Fatal(err)
	}
	next, err := m.RegenerateRecovery(ctx, tokens.AccessToken)
	if err != nil || len(next) != 10 {
		t.Fatal(err)
	}
	if err = m.Verify(ctx, tokens.AccessToken, codes[0], true); !errors.Is(err, ErrMFACode) {
		t.Fatal("rotated recovery code active", err)
	}
	if err = m.Disable(ctx, tokens.AccessToken); err != nil {
		t.Fatal(err)
	}
	if err = m.Verify(ctx, tokens.AccessToken, next[0], true); !errors.Is(err, ErrMFACode) {
		t.Fatal("disabled recovery active", err)
	}
	status, err := m.Status(ctx, tokens.AccessToken)
	if err != nil || status.Enabled {
		t.Fatal(status, err)
	}
	enrollment, err := m.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(10 * time.Minute)
	refreshed, err := s.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE auth_sessions SET authenticated_at=$1 WHERE account_id=(SELECT id FROM accounts WHERE email='mfa@example.invalid')`, *now); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Confirm(ctx, refreshed.AccessToken, enrollment.ID, otpCode(t, enrollment.Secret, *now)); !errors.Is(err, ErrMFACode) {
		t.Fatal("expired enrollment accepted", err)
	}
}

func TestMFAConcurrentRecoveryAndTamperResistance(t *testing.T) {
	db, s, m, tokens, now := mfaFixture(t)
	ctx := context.Background()
	setup, err := m.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := m.Confirm(ctx, tokens.AccessToken, setup.ID, otpCode(t, setup.Secret, *now))
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- m.Verify(ctx, tokens.AccessToken, codes[0], true) }()
	}
	successes := 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrMFACode) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("concurrent recovery", successes)
	}
	other, err := NewMFAService(db, s, []byte(strings.Repeat("z", 32)))
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(30 * time.Second)
	if err = other.Verify(ctx, tokens.AccessToken, otpCode(t, setup.Secret, *now), false); err == nil {
		t.Fatal("wrong encryption key accepted")
	}
	if _, err = db.Exec(`UPDATE mfa_factors SET sealed_secret='corrupted'::bytea`); err != nil {
		t.Fatal(err)
	}
	if err = m.Verify(ctx, tokens.AccessToken, otpCode(t, setup.Secret, *now), false); err == nil {
		t.Fatal("tampered factor accepted")
	}
	if err = s.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Status(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("revoked status", err)
	}
}
func TestMFAGoogleLinkNeedsSecondFactor(t *testing.T) {
	_, s, m, tokens, now := mfaFixture(t)
	ctx := context.Background()
	a, err := s.Authenticate(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := m.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Confirm(ctx, tokens.AccessToken, setup.ID, otpCode(t, setup.Secret, *now)); err != nil {
		t.Fatal(err)
	}
	g := NewGoogleService(m.db, s, &fakeGoogle{}, false, func() time.Time { return *now })
	if _, err = g.Begin(ctx, "browser", second.AccessToken); !errors.Is(err, ErrMFARequired) {
		t.Fatal("Google link bypassed MFA", err)
	}
}

func TestMFAGoogleLinkRechecksProofAtCallback(t *testing.T) {
	db, s, m, tokens, now := mfaFixture(t)
	ctx := context.Background()
	a, err := s.Authenticate(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := m.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := m.Confirm(ctx, tokens.AccessToken, setup.ID, otpCode(t, setup.Secret, *now))
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeGoogle{identity: GoogleIdentity{Subject: "new-linked-subject", Email: a.Email, EmailVerified: true}}
	g := NewGoogleService(db, s, p, false, func() time.Time { return *now })
	flow, err := g.Begin(ctx, "browser", tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Verify(ctx, second.AccessToken, codes[0], true); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(flow.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.Complete(ctx, u.Query().Get("state"), "browser", "valid"); !errors.Is(err, ErrMFARequired) {
		t.Fatal("callback accepted invalidated proof", err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM external_identities`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed proof linked identity", count, err)
	}
}

func TestMFALinkCallbackCannotOutlivePrimaryProofWhileWaiting(t *testing.T) {
	db, s, m, tokens, now := mfaFixture(t)
	ctx := context.Background()
	var clock atomic.Int64
	clock.Store(now.Unix())
	s.now = func() time.Time { return time.Unix(clock.Load(), 0).UTC() }
	a, err := s.Authenticate(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := m.Begin(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Confirm(ctx, tokens.AccessToken, setup.ID, otpCode(t, setup.Secret, s.now())); err != nil {
		t.Fatal(err)
	}
	clock.Add(240)
	if err = m.Verify(ctx, tokens.AccessToken, otpCode(t, setup.Secret, s.now()), false); err != nil {
		t.Fatal(err)
	}
	p := &fakeGoogle{identity: GoogleIdentity{Subject: "waiting-linked", Email: a.Email, EmailVerified: true}}
	g := NewGoogleService(db, s, p, false, s.now)
	flow, err := g.Begin(ctx, "browser", tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(flow.URL)
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if _, err = holder.ExecContext(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, a.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := g.Complete(ctx, u.Query().Get("state"), "browser", "valid"); result <- err }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting bool
		if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("callback did not reach account lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	clock.Add(120)
	if err = holder.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrInvalidSession) {
		t.Fatal("callback outlived primary proof", err)
	}
}
