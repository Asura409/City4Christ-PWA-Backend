package auth

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPasswordRecovery(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	v, _ := NewVerificationService(db, VerificationConfig{Key: bytes.Repeat([]byte{42}, 32), Now: func() time.Time { return now }})
	s := NewPasswordRecoveryService(db, v)
	hash, _ := NewPasswordHasher().Hash("original long password")
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "recover@example.invalid", FirstName: "Test", LastName: "Recovery", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET email_verified_at=$2 WHERE id=$1", a.ID, now); err != nil {
		t.Fatal(err)
	}
	sessions, _ := NewSessionService(db, SessionConfig{Key: bytes.Repeat([]byte{42}, 32), Now: func() time.Time { return now }})
	old, err := sessions.Login(ctx, a.Email, "original long password")
	if err != nil {
		t.Fatal(err)
	}
	readToken := func() string {
		t.Helper()
		var id string
		var sealed []byte
		if err := db.QueryRow("SELECT id,sealed_payload FROM password_recovery_outbox WHERE account_id=$1 AND kind='reset'", a.ID).Scan(&id, &sealed); err != nil {
			t.Fatal(err)
		}
		m, err := s.OpenEmail(a.ID, id, sealed)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(m.ResetURL)
		if err != nil {
			t.Fatal(err)
		}
		token := u.Fragment[len("reset_token="):]
		if len(token) != 43 || bytes.Contains(sealed, []byte(token)) {
			t.Fatal("insecure reset token storage")
		}
		if _, err = s.OpenEmail(a.ID, "wrong-id", sealed); err == nil {
			t.Fatal("unbound ciphertext")
		}
		return token
	}
	request := func() {
		t.Helper()
		if err := s.Request(ctx, a.Email, "https://frontend.example.invalid/auth.html"); err != nil {
			t.Fatal(err)
		}
	}
	request()
	first := readToken()
	request()
	if readToken() != first {
		t.Fatal("cooldown replaced token")
	}
	if _, err = sessions.Authenticate(ctx, old.AccessToken); err != nil {
		t.Fatal("request revoked session")
	}
	if err = s.Reset(ctx, first, "short"); err == nil {
		t.Fatal("weak password accepted")
	}
	now = now.Add(time.Minute)
	request()
	token := readToken()
	if token == first {
		t.Fatal("token reused")
	}
	if err = s.Reset(ctx, first, "replacement long password"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("superseded token: %v", err)
	}
	var succeeded atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Reset(ctx, token, "replacement long password")
			if err == nil {
				succeeded.Add(1)
			} else if !errors.Is(err, ErrInvalidResetToken) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if succeeded.Load() != 1 {
		t.Fatal("token not single-use")
	}
	if _, err = sessions.Authenticate(ctx, old.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("old access remains valid")
	}
	if _, err = sessions.Refresh(ctx, old.RefreshToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("old refresh remains valid")
	}
	if _, err = sessions.Login(ctx, a.Email, "original long password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("old password accepted")
	}
	if _, err = sessions.Login(ctx, a.Email, "replacement long password"); err != nil {
		t.Fatal(err)
	}
	var notifications int
	if err = db.QueryRow("SELECT count(*) FROM password_recovery_outbox WHERE kind='changed'").Scan(&notifications); err != nil || notifications != 1 {
		t.Fatal("notification missing", err)
	}
	now = now.Add(time.Minute)
	request()
	token = readToken()
	now = now.Add(15 * time.Minute)
	if err = s.Reset(ctx, token, "another long password"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatal("expired token accepted")
	}
	for _, email := range []string{"unknown@example.invalid", "google@example.invalid", "unverified@example.invalid"} {
		if email != "unknown@example.invalid" {
			input := NewAccount{Email: email, FirstName: "Other", LastName: "User"}
			if email == "unverified@example.invalid" {
				input.PasswordHash = &hash
			}
			if _, err := NewAccountRepository(db).Create(ctx, input); err != nil {
				t.Fatal(err)
			}
			if email == "google@example.invalid" {
				if _, err := db.Exec("UPDATE accounts SET email_verified_at=$2 WHERE email=$1", email, now); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err = s.Request(ctx, email, "https://frontend.example.invalid/auth.html"); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM password_recoveries").Scan(&count); err != nil || count != 1 {
		t.Fatal("ineligible account issued token", err)
	}
}

// A login that checked the old hash before recovery must not create a new,
// unrevoked session after recovery commits.
func TestPasswordRecoveryRejectsInFlightOldPasswordLogin(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	hash, _ := NewPasswordHasher().Hash("original long password")
	replacement, _ := NewPasswordHasher().Hash("replacement long password")
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "race@example.invalid", FirstName: "Test", LastName: "User", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET email_verified_at=now() WHERE id=$1", a.ID); err != nil {
		t.Fatal(err)
	}
	s, _ := NewSessionService(db, SessionConfig{Key: bytes.Repeat([]byte{42}, 32)})
	// Warm the dummy hash so the blocked query below represents session creation.
	_, _ = s.Login(ctx, "missing@example.invalid", "original long password")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("SELECT id FROM accounts WHERE id=$1 FOR UPDATE", a.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Login(ctx, a.Email, "original long password"); done <- err }()
	// Race-instrumented Argon2id is slower on the shared VPS. Wait for the
	// observable lock event rather than assuming normal-build hash timing.
	deadline := time.Now().Add(time.Minute)
	for {
		var blocked bool
		if err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock')").Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("login did not reach locked account")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = tx.Exec("UPDATE accounts SET password_hash=$2 WHERE id=$1", a.ID, replacement); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("in-flight old-password login survived recovery: %v", err)
	}
}

func TestPasswordRecoveryRollsBackIfNotificationCannotQueue(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	key := bytes.Repeat([]byte{42}, 32)
	v, _ := NewVerificationService(db, VerificationConfig{Key: key, Now: func() time.Time { return now }})
	s := NewPasswordRecoveryService(db, v)
	hash, _ := NewPasswordHasher().Hash("original long password")
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "atomic@example.invalid", FirstName: "Atomic", LastName: "Recovery", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET email_verified_at=$2 WHERE id=$1", a.ID, now); err != nil {
		t.Fatal(err)
	}
	sessions, _ := NewSessionService(db, SessionConfig{Key: key, Now: func() time.Time { return now }})
	old, err := sessions.Login(ctx, a.Email, "original long password")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Request(ctx, a.Email, "https://frontend.example.invalid/auth.html"); err != nil {
		t.Fatal(err)
	}
	var id string
	var sealed []byte
	if err = db.QueryRow("SELECT id,sealed_payload FROM password_recovery_outbox WHERE account_id=$1", a.ID).Scan(&id, &sealed); err != nil {
		t.Fatal(err)
	}
	m, err := s.OpenEmail(a.ID, id, sealed)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(m.ResetURL)
	token := u.Fragment[len("reset_token="):]
	// Simulate an outbox constraint failure after the password/session updates.
	if _, err = db.Exec(`ALTER TABLE password_recovery_outbox ADD CONSTRAINT reject_changed_for_test CHECK(kind<>'changed')`); err != nil {
		t.Fatal(err)
	}
	if err = s.Reset(ctx, token, "replacement long password"); err == nil {
		t.Fatal("expected notification insert failure")
	}
	if _, err = sessions.Authenticate(ctx, old.AccessToken); err != nil {
		t.Fatal("failed reset revoked session")
	}
	if _, err = sessions.Login(ctx, a.Email, "original long password"); err != nil {
		t.Fatal("failed reset changed password")
	}
	if _, err = db.Exec("ALTER TABLE password_recovery_outbox DROP CONSTRAINT reject_changed_for_test"); err != nil {
		t.Fatal(err)
	}
	if err = s.Reset(ctx, token, "replacement long password"); err != nil {
		t.Fatal("failed reset consumed token", err)
	}
}
