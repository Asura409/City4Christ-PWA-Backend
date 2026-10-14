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

type fakeGoogle struct {
	nonce, verifier string
	identity        GoogleIdentity
}

func (p *fakeGoogle) AuthURL(state, nonce, verifier string) string {
	p.nonce, p.verifier = nonce, verifier
	return "https://accounts.google.com/auth?state=" + url.QueryEscape(state)
}
func (p *fakeGoogle) Exchange(ctx context.Context, code, nonce, verifier string) (GoogleIdentity, error) {
	if code != "valid" || nonce != p.nonce || verifier != p.verifier {
		return GoogleIdentity{}, ErrInvalidOIDC
	}
	return p.identity, nil
}
func TestGoogleSignupLoginAndExplicitLinking(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sessions, err := NewSessionService(db, SessionConfig{Key: []byte(strings.Repeat("k", 32)), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeGoogle{identity: GoogleIdentity{Subject: "google-subject-1", Email: "google@example.invalid", EmailVerified: true, FirstName: "Google", LastName: "Tester"}}
	google := NewGoogleService(db, sessions, provider, true, func() time.Time { return now })
	begin := func(link string) (GoogleFlow, string) {
		flow, err := google.Begin(ctx, "browser-secret", link)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(flow.URL)
		return flow, u.Query().Get("state")
	}
	_, state := begin("")
	if _, err := google.Complete(ctx, state, "other-browser", "valid"); !errors.Is(err, ErrInvalidOIDC) {
		t.Fatal("browser binding bypass", err)
	}
	tokens, err := google.Complete(ctx, state, "browser-secret", "valid")
	if err != nil {
		t.Fatal(err)
	}
	account, err := sessions.Authenticate(ctx, tokens.AccessToken)
	if err != nil || account.PasswordHash != nil || account.EmailVerifiedAt == nil {
		t.Fatal("invalid Google-only account", err)
	}
	if _, err := google.Complete(ctx, state, "browser-secret", "valid"); !errors.Is(err, ErrInvalidOIDC) {
		t.Fatal("OIDC replay", err)
	}
	_, state = begin("")
	if _, err := google.Complete(ctx, state, "browser-secret", "valid"); err != nil {
		t.Fatal("existing Google login", err)
	}
	// A Google assertion with the same email must never silently link a local account.
	hash, err := NewPasswordHasher().Hash("local account long password")
	if err != nil {
		t.Fatal(err)
	}
	local, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "local@example.invalid", FirstName: "Local", LastName: "Tester", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=$1 WHERE id=$2`, now, local.ID); err != nil {
		t.Fatal(err)
	}
	provider.identity = GoogleIdentity{Subject: "google-subject-2", Email: local.Email, EmailVerified: true, FirstName: "Local", LastName: "Tester"}
	_, state = begin("")
	if _, err := google.Complete(ctx, state, "browser-secret", "valid"); !errors.Is(err, ErrLinkRequired) {
		t.Fatal("email auto-linked", err)
	}
	login, err := sessions.Login(ctx, local.Email, "local account long password")
	if err != nil {
		t.Fatal(err)
	}
	_, state = begin(login.AccessToken)
	linked, err := google.Complete(ctx, state, "browser-secret", "valid")
	if err != nil {
		t.Fatal(err)
	}
	got, err := sessions.Authenticate(ctx, linked.AccessToken)
	if err != nil || got.ID != local.ID || got.PasswordHash == nil {
		t.Fatal("secure linking changed account/password", err)
	}
	_, state = begin("")
	now = now.Add(10 * time.Minute)
	if _, err := google.Complete(ctx, state, "browser-secret", "valid"); !errors.Is(err, ErrInvalidOIDC) {
		t.Fatal("expired flow", err)
	}
}
func TestGoogleClosedSignupUnverifiedEmailAndRevokedLink(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	sessions, err := NewSessionService(db, SessionConfig{Key: make([]byte, 32)})
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeGoogle{identity: GoogleIdentity{Subject: "subject", Email: "new@example.invalid", EmailVerified: true}}
	g := NewGoogleService(db, sessions, p, false, nil)
	f, err := g.Begin(ctx, "browser", "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.URL)
	if _, err := g.Complete(ctx, u.Query().Get("state"), "browser", "valid"); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatal("closed signup bypass", err)
	}
	p.identity.EmailVerified = false
	g = NewGoogleService(db, sessions, p, true, nil)
	f, err = g.Begin(ctx, "browser", "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ = url.Parse(f.URL)
	if _, err := g.Complete(ctx, u.Query().Get("state"), "browser", "valid"); !errors.Is(err, ErrInvalidOIDC) {
		t.Fatal("unverified Google email accepted", err)
	}
}

func TestGoogleLinkRequiresRecentActiveMatchingAccount(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	s, err := NewSessionService(db, SessionConfig{Key: make([]byte, 32), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "link@example.invalid", FirstName: "Test", LastName: "User"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=$1 WHERE id=$2`, now, a.ID); err != nil {
		t.Fatal(err)
	}
	p := &fakeGoogle{identity: GoogleIdentity{Subject: "linked-subject", Email: a.Email, EmailVerified: true}}
	g := NewGoogleService(db, s, p, true, func() time.Time { return now })
	tokens, err := s.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	flow, err := g.Begin(ctx, "browser", tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(flow.URL)
	if err := s.Logout(ctx, tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Complete(ctx, u.Query().Get("state"), "browser", "valid"); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("revoked session authorized link", err)
	}
	tokens, err = s.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	p.identity.Email = "different@example.invalid"
	flow, err = g.Begin(ctx, "browser", tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	u, _ = url.Parse(flow.URL)
	if _, err := g.Complete(ctx, u.Query().Get("state"), "browser", "valid"); !errors.Is(err, ErrInvalidOIDC) {
		t.Fatal("mismatched email linked", err)
	}
	now = now.Add(5 * time.Minute)
	if _, err := g.Begin(ctx, "browser", tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("old login authorized linking", err)
	}
}
