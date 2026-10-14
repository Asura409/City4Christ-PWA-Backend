package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPasswordSessionsRotationReplayAndLogout(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	service, err := NewSessionService(db, SessionConfig{Key: []byte(strings.Repeat("k", 32)), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	password := "a long session test password"
	hash, err := NewPasswordHasher().Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	account, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "session@example.invalid", FirstName: "Test", LastName: "User", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{account.Email, "missing@example.invalid"} {
		if _, err := service.Login(ctx, email, "incorrect"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("incorrect login: %v", err)
		}
	}
	if _, err := service.Login(ctx, account.Email, password); !errors.Is(err, ErrEmailUnverified) {
		t.Fatalf("unverified: %v", err)
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=$1 WHERE id=$2`, now, account.ID); err != nil {
		t.Fatal(err)
	}
	tokens, err := service.Login(ctx, " SESSION@EXAMPLE.INVALID ", password)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.RefreshToken == "" || tokens.AccessToken == "" || tokens.AccessExpiresAt.Sub(now) != 10*time.Minute {
		t.Fatal("invalid token lifetimes")
	}
	got, err := service.Authenticate(ctx, tokens.AccessToken)
	if err != nil || got.ID != account.ID {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := service.Authenticate(ctx, tokens.AccessToken+"x"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("tampered token: %v", err)
	}
	var digest []byte
	if err := db.QueryRow(`SELECT token_digest FROM refresh_tokens`).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if string(digest) == tokens.RefreshToken {
		t.Fatal("raw refresh stored")
	}
	now = now.Add(10 * time.Minute)
	if _, err := service.Authenticate(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expired access: %v", err)
	}
	rotated, err := service.Refresh(ctx, tokens.RefreshToken)
	if err != nil || rotated.RefreshToken == tokens.RefreshToken {
		t.Fatalf("rotation: %v", err)
	}
	if _, err := service.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrRefreshReplay) {
		t.Fatalf("replay: %v", err)
	}
	if _, err := service.Refresh(ctx, rotated.RefreshToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("replay family active: %v", err)
	}
	if _, err := service.Authenticate(ctx, rotated.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("replayed access active: %v", err)
	}
	fresh, err := service.Login(ctx, account.Email, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(ctx, fresh.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(ctx, fresh.RefreshToken); err != nil {
		t.Fatal("logout not idempotent", err)
	}
	if _, err := service.Authenticate(ctx, fresh.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("logout left access usable")
	}
}

func TestGoogleOnlyAndSessionAbsoluteExpiry(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	service, err := NewSessionService(db, SessionConfig{Key: []byte(strings.Repeat("k", 32)), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "google@example.invalid", FirstName: "Google", LastName: "User"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(ctx, a.Email, "anything"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("passwordless login accepted", err)
	}
	tokens, err := service.Start(ctx, a.ID)
	if !errors.Is(err, ErrEmailUnverified) {
		t.Fatal("unverified session accepted", tokens, err)
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=$1 WHERE id=$2`, now, a.ID); err != nil {
		t.Fatal(err)
	}
	tokens, err = service.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * 24 * time.Hour)
	if _, err := service.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("expired refresh accepted", err)
	}
}

func TestConcurrentRefreshAndRollback(t *testing.T) {
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
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "concurrent@example.invalid", FirstName: "Test", LastName: "User"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET email_verified_at=$1 WHERE id=$2`, now, a.ID); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		tokens SessionTokens
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() { v, e := s.Refresh(ctx, tokens.RefreshToken); results <- outcome{v, e} }()
	}
	first, second := <-results, <-results
	if first.err != nil {
		first, second = second, first
	}
	if first.err != nil || !errors.Is(second.err, ErrRefreshReplay) {
		t.Fatalf("concurrent rotation: %v %v", first.err, second.err)
	}
	if _, err := s.Authenticate(ctx, first.tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("concurrent replay did not revoke family")
	}
	fresh, err := s.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE FUNCTION fail_refresh_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated'; END $$; CREATE TRIGGER fail_refresh_insert BEFORE INSERT ON refresh_tokens FOR EACH ROW EXECUTE FUNCTION fail_refresh_insert()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Refresh(ctx, fresh.RefreshToken); err == nil {
		t.Fatal("simulated failure ignored")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_refresh_insert ON refresh_tokens`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Refresh(ctx, fresh.RefreshToken); err != nil {
		t.Fatal("failed rotation consumed original token", err)
	}
}
