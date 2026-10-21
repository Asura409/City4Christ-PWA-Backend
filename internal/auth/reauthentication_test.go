package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPasswordReauthenticationIsSessionBoundAndExpires(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	s, err := NewSessionService(db, SessionConfig{Key: []byte(strings.Repeat("r", 32)), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := NewPasswordHasher().Hash("synthetic reauthentication password")
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "reauth@example.invalid", FirstName: "Reauth", LastName: "Test", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE accounts SET email_verified_at=$2 WHERE id=$1`, a.ID, now); err != nil {
		t.Fatal(err)
	}
	first, err := s.Login(ctx, a.Email, "synthetic reauthentication password")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Login(ctx, a.Email, "synthetic reauthentication password")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Minute)
	if err = s.RequireFresh(ctx, first.AccessToken); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("old login fresh", err)
	}
	rotated, err := s.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RequireFresh(ctx, rotated.AccessToken); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("refresh renewed freshness", err)
	}
	if _, err = s.ReauthenticatePassword(ctx, rotated.AccessToken, "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("bad password accepted", err)
	}
	if _, err = s.ReauthenticatePassword(ctx, rotated.AccessToken, "synthetic reauthentication password"); err != nil {
		t.Fatal(err)
	}
	if err = s.RequireFresh(ctx, rotated.AccessToken); err != nil {
		t.Fatal("confirmation not effective", err)
	}
	if err = s.RequireFresh(ctx, second.AccessToken); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("other session became fresh", err)
	}
	status, err := s.ReauthenticationStatus(ctx, rotated.AccessToken)
	if err != nil || !status.Fresh || !status.PasswordAvailable || status.GoogleAvailable || status.FreshUntil == nil || !status.FreshUntil.Equal(now.Add(5*time.Minute)) {
		t.Fatal("incorrect status", status, err)
	}
	now = now.Add(5 * time.Minute)
	rotated, err = s.Refresh(ctx, rotated.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RequireFresh(ctx, rotated.AccessToken); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("boundary expiry accepted", err)
	}
	if err = s.Logout(ctx, rotated.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReauthenticatePassword(ctx, rotated.AccessToken, "synthetic reauthentication password"); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("revoked session confirmed", err)
	}
}

func TestGoogleOnlyCannotReauthenticateWithPassword(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('google-reauth@example.invalid','Google','Only',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO external_identities(provider,subject,account_id) VALUES('google','reauth-subject',$1)`, id); err != nil {
		t.Fatal(err)
	}
	s, _ := NewSessionService(db, SessionConfig{Key: make([]byte, 32)})
	tokens, err := s.Start(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReauthenticatePassword(ctx, tokens.AccessToken, "synthetic password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("Google password invented", err)
	}
	status, err := s.ReauthenticationStatus(ctx, tokens.AccessToken)
	if err != nil || status.PasswordAvailable || !status.GoogleAvailable {
		t.Fatal(status, err)
	}
}

func TestFreshPasswordConfirmationAllowsOldSessionGoogleLink(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	s, _ := NewSessionService(db, SessionConfig{Key: make([]byte, 32), Now: func() time.Time { return now }})
	hash, err := NewPasswordHasher().Hash("synthetic linking confirmation")
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "fresh-link@example.invalid", FirstName: "Fresh", LastName: "Link", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE accounts SET email_verified_at=$2 WHERE id=$1`, a.ID, now); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.Login(ctx, a.Email, "synthetic linking confirmation")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	p := &fakeGoogle{identity: GoogleIdentity{Subject: "new-link-subject", Email: a.Email, EmailVerified: true}}
	g := NewGoogleService(db, s, p, false, func() time.Time { return now })
	if _, err = g.Begin(ctx, "browser", tokens.AccessToken); err == nil {
		t.Fatal("old session linked without confirmation")
	}
	if _, err = s.ReauthenticatePassword(ctx, tokens.AccessToken, "synthetic linking confirmation"); err != nil {
		t.Fatal(err)
	}
	flow, err := g.Begin(ctx, "browser", tokens.AccessToken)
	if err != nil {
		t.Fatal("fresh confirmation did not unlock linking", err)
	}
	u, _ := url.Parse(flow.URL)
	if _, err = g.Complete(ctx, u.Query().Get("state"), "browser", "valid"); err != nil {
		t.Fatal("link callback ignored confirmation", err)
	}
}

func TestReauthenticationAuditFailureRollsBackAndFreshGuardLocksSession(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	s, _ := NewSessionService(db, SessionConfig{Key: make([]byte, 32), Now: func() time.Time { return now }})
	hash, err := NewPasswordHasher().Hash("synthetic atomic confirmation")
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err = db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at,password_hash) VALUES('atomic-reauth@example.invalid','Atomic','Test',$1,$2) RETURNING id`, now, hash).Scan(&id); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.Start(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	if _, err = db.Exec(`ALTER TABLE reauthentication_audit_events RENAME TO unavailable_audit`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReauthenticatePassword(ctx, tokens.AccessToken, "synthetic atomic confirmation"); err == nil {
		t.Fatal("confirmation committed without audit")
	}
	if err = s.RequireFresh(ctx, tokens.AccessToken); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("audit failure left fresh state", err)
	}
	if _, err = db.Exec(`ALTER TABLE unavailable_audit RENAME TO reauthentication_audit_events`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReauthenticatePassword(ctx, tokens.AccessToken, "synthetic atomic confirmation"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = s.RequireFreshTx(ctx, tx, tokens.AccessToken); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- s.Logout(ctx, tokens.RefreshToken) }()
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
			t.Fatal("logout did not wait for sensitive transaction")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	if err = s.RequireFresh(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("logout did not invalidate confirmation", err)
	}
}

func TestFreshGuardRechecksDeadlineAfterWaitingForSessionLock(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(time.Now().Unix())
	s, _ := NewSessionService(db, SessionConfig{Key: make([]byte, 32), Now: func() time.Time { return time.Unix(clock.Load(), 0).UTC() }})
	var id string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('waiting-reauth@example.invalid','Waiting','Test',now()) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.Start(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if _, err = holder.ExecContext(ctx, `SELECT id FROM auth_sessions WHERE account_id=$1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			result <- err
			return
		}
		defer tx.Rollback()
		result <- s.RequireFreshTx(ctx, tx, tokens.AccessToken)
	}()
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
			t.Fatal("guard did not wait for lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	clock.Add(int64(6 * time.Minute / time.Second))
	if err = holder.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("proof expired while waiting but authorized action", err)
	}
}
