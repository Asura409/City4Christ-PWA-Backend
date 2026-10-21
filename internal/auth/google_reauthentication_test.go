package auth

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestGoogleReauthenticationBindsIdentitySessionBrowserAndFreshAuthTime(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	s, _ := NewSessionService(db, SessionConfig{Key: []byte(strings.Repeat("g", 32)), Now: func() time.Time { return now }})
	var id string
	if err := db.QueryRow(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('google-fresh@example.invalid','Google','Fresh',$1) RETURNING id`, now).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO external_identities(provider,subject,account_id) VALUES('google','linked-subject',$1)`, id); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.Start(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	p := &fakeGoogle{identity: GoogleIdentity{Subject: "linked-subject", Email: "google-fresh@example.invalid", EmailVerified: true, AuthenticatedAt: now}}
	g := NewGoogleService(db, s, p, false, func() time.Time { return now })
	begin := func() string {
		t.Helper()
		flow, err := g.BeginReauthentication(ctx, "browser", tokens.AccessToken, "https://front.example/auth.html")
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(flow.URL)
		if u.Query().Get("max_age") != "0" || u.Query().Get("claims") == "" {
			t.Fatal("fresh provider request missing")
		}
		return u.Query().Get("state")
	}
	state := begin()
	handled, _, err := g.CompleteReauthentication(ctx, state, "wrong-browser", "valid")
	if !handled || !errors.Is(err, ErrInvalidOIDC) {
		t.Fatal("browser bypass", handled, err)
	}
	// A reauthentication state must not fall through to signup/link/login.
	if _, _, err := g.CompleteWithReturn(ctx, state, "browser", "valid"); !errors.Is(err, ErrInvalidOIDC) {
		t.Fatal("reauth flow accepted by login", err)
	}
	for _, tc := range []struct {
		name, subject string
		at            time.Time
		verified      bool
	}{
		{"wrong subject", "different", now, true}, {"no auth_time", "linked-subject", time.Time{}, true}, {"old auth_time", "linked-subject", now.Add(-time.Hour), true}, {"future auth_time", "linked-subject", now.Add(time.Minute), true}, {"unverified", "linked-subject", now, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := begin()
			p.identity.Subject = tc.subject
			p.identity.AuthenticatedAt = tc.at
			p.identity.EmailVerified = tc.verified
			handled, _, err := g.CompleteReauthentication(ctx, state, "browser", "valid")
			if !handled || err == nil {
				t.Fatal("bad confirmation accepted", err)
			}
			if err = s.RequireFresh(ctx, tokens.AccessToken); !errors.Is(err, ErrReauthenticationRequired) {
				t.Fatal("failed proof freshened session", err)
			}
		})
	}
	p.identity.Subject = "linked-subject"
	p.identity.AuthenticatedAt = now
	p.identity.EmailVerified = true
	state = begin()
	handled, target, err := g.CompleteReauthentication(ctx, state, "browser", "valid")
	if !handled || err != nil || target != "https://front.example/auth.html" {
		t.Fatal("valid confirmation", handled, target, err)
	}
	if err = s.RequireFresh(ctx, tokens.AccessToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err = g.CompleteReauthentication(ctx, state, "browser", "valid"); !errors.Is(err, ErrInvalidOIDC) {
		t.Fatal("replay accepted", err)
	}
	var sessions, identities, audits int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM auth_sessions),(SELECT count(*) FROM external_identities),(SELECT count(*) FROM reauthentication_audit_events)`).Scan(&sessions, &identities, &audits); err != nil || sessions != 1 || identities != 1 || audits != 1 {
		t.Fatal("confirmation changed login/identity or audit", sessions, identities, audits, err)
	}
	state = begin()
	expiredNow := now.Add(ReauthenticationLifetime)
	now = expiredNow
	if _, _, err = g.CompleteReauthentication(ctx, state, "browser", "valid"); !errors.Is(err, ErrInvalidOIDC) {
		t.Fatal("expired provider flow accepted", err)
	}
	renewed, refreshErr := s.Refresh(ctx, tokens.RefreshToken)
	if refreshErr != nil {
		t.Fatal(refreshErr)
	}
	tokens = renewed
	p.identity.AuthenticatedAt = now
	state = begin()
	if err = s.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, _, err = g.CompleteReauthentication(ctx, state, "browser", "valid"); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("logout not enforced", err)
	}
}

func TestOrdinaryGoogleLoginCannotBypassFreshConfirmation(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	s, _ := NewSessionService(db, SessionConfig{Key: make([]byte, 32)})
	p := &fakeGoogle{identity: GoogleIdentity{Subject: "ordinary-sso", Email: "ordinary-sso@example.invalid", EmailVerified: true}}
	g := NewGoogleService(db, s, p, true, nil)
	flow, err := g.Begin(ctx, "browser", "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(flow.URL)
	tokens, err := g.Complete(ctx, u.Query().Get("state"), "browser", "valid")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RequireFresh(ctx, tokens.AccessToken); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("ordinary SSO without auth_time bypassed freshness", err)
	}
}
