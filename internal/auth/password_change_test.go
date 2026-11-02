package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func passwordChangeFixture(t *testing.T) (*sql.DB, *PasswordChangeService, *SessionService, Account, SessionTokens) {
	t.Helper()
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{73}, 32)
	v, err := NewVerificationService(db, VerificationConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := NewSessionService(db, SessionConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := NewPasswordHasher().Hash("original long password")
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "change@example.invalid", FirstName: "Synthetic", LastName: "Member", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET email_verified_at=now() WHERE id=$1", a.ID); err != nil {
		t.Fatal(err)
	}
	tokens, err := sessions.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	return db, NewPasswordChangeService(db, sessions, NewPasswordRecoveryService(db, v)), sessions, a, tokens
}

func TestPasswordChangeRevokesEverySessionAndRecovery(t *testing.T) {
	db, s, sessions, a, first := passwordChangeFixture(t)
	ctx := context.Background()
	second, err := sessions.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.recovery.Request(ctx, a.Email, "https://frontend.example.invalid/auth.html"); err != nil {
		t.Fatal(err)
	}
	var id string
	var sealed []byte
	if err = db.QueryRow("SELECT id,sealed_payload FROM password_recovery_outbox WHERE account_id=$1 AND kind='reset'", a.ID).Scan(&id, &sealed); err != nil {
		t.Fatal(err)
	}
	message, err := s.recovery.OpenEmail(a.ID, id, sealed)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(message.ResetURL)
	reset := strings.TrimPrefix(u.Fragment, "reset_token=")
	if err = s.Change(ctx, first.AccessToken, "original long password", "replacement long password"); err != nil {
		t.Fatal(err)
	}
	for _, tokens := range []SessionTokens{first, second} {
		if _, err = sessions.Authenticate(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("access survived: %v", err)
		}
		if _, err = sessions.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("refresh survived: %v", err)
		}
	}
	if err = s.recovery.Reset(ctx, reset, "another replacement password"); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("recovery survived: %v", err)
	}
	if _, err = sessions.Login(ctx, a.Email, "original long password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password survived: %v", err)
	}
	if _, err = sessions.Login(ctx, a.Email, "replacement long password"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM password_recovery_outbox WHERE account_id=$1 AND kind='reset'", a.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("reset email survived", count, err)
	}
	if err = db.QueryRow("SELECT id,sealed_payload FROM password_recovery_outbox WHERE account_id=$1 AND kind='changed'", a.ID).Scan(&id, &sealed); err != nil {
		t.Fatal(err)
	}
	message, err = s.recovery.OpenEmail(a.ID, id, sealed)
	if err != nil || !message.PasswordChanged || message.Email != a.Email || message.ResetURL != "" || bytes.Contains(sealed, []byte(a.Email)) {
		t.Fatal("invalid encrypted changed notice", err)
	}
}

func TestPasswordChangeDenialsPreserveCredentials(t *testing.T) {
	db, s, sessions, a, tokens := passwordChangeFixture(t)
	ctx := context.Background()
	if err := s.Change(ctx, tokens.AccessToken, "wrong password", "replacement long password"); !errors.Is(err, ErrCurrentPasswordInvalid) {
		t.Fatalf("wrong current: %v", err)
	}
	for _, password := range []string{"short", strings.Repeat("x", 1025), string([]byte{0xff}) + strings.Repeat("x", 15), "original long password"} {
		err := s.Change(ctx, tokens.AccessToken, "original long password", password)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["newPassword"] == "" {
			t.Fatalf("policy accepted %d bytes: %v", len(password), err)
		}
	}
	if _, err := sessions.Authenticate(ctx, tokens.AccessToken); err != nil {
		t.Fatal("denial revoked session", err)
	}
	if _, err := db.Exec("UPDATE accounts SET password_hash=NULL WHERE id=$1", a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Change(ctx, tokens.AccessToken, "original long password", "replacement long password"); !errors.Is(err, ErrLocalPasswordRequired) {
		t.Fatalf("Google-only gained password: %v", err)
	}
	if _, err := db.Exec("UPDATE accounts SET email_verified_at=NULL WHERE id=$1", a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Change(ctx, tokens.AccessToken, "original long password", "replacement long password"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("unverified session: %v", err)
	}
}

func TestPasswordChangeRollsBackWhenNoticeCannotQueue(t *testing.T) {
	db, s, sessions, a, tokens := passwordChangeFixture(t)
	ctx := context.Background()
	if err := s.recovery.Request(ctx, a.Email, "https://frontend.example.invalid/auth.html"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE password_recovery_outbox ADD CONSTRAINT reject_change_test CHECK(kind<>'changed')"); err != nil {
		t.Fatal(err)
	}
	if err := s.Change(ctx, tokens.AccessToken, "original long password", "replacement long password"); err == nil {
		t.Fatal("accepted failed notice")
	}
	if _, err := sessions.Authenticate(ctx, tokens.AccessToken); err != nil {
		t.Fatal("failure revoked session", err)
	}
	var original string
	if err := db.QueryRow("SELECT password_hash FROM accounts WHERE id=$1", a.ID).Scan(&original); err != nil || !NewPasswordHasher().Verify("original long password", original) {
		t.Fatal("failure changed password", err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM password_recoveries WHERE account_id=$1 AND consumed_at IS NULL AND token_digest IS NOT NULL", a.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("failure consumed reset", err)
	}
}

func TestPasswordChangeConcurrentChangesHaveOneWinner(t *testing.T) {
	db, s, sessions, a, first := passwordChangeFixture(t)
	ctx := context.Background()
	second, err := sessions.Start(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, tokens := range []SessionTokens{first, second} {
		wg.Add(1)
		go func(access string) {
			defer wg.Done()
			results <- s.Change(ctx, access, "original long password", "replacement long password")
		}(tokens.AccessToken)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrInvalidSession) && !errors.Is(err, ErrCurrentPasswordInvalid) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("concurrent winners", wins)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM password_recovery_outbox WHERE kind='changed'").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate notice", count, err)
	}
}

func TestPasswordChangeRechecksSessionAfterAccountLock(t *testing.T) {
	db, s, _, a, tokens := passwordChangeFixture(t)
	ctx := context.Background()
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if _, err = holder.Exec("SELECT id FROM accounts WHERE id=$1 FOR UPDATE", a.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.Change(ctx, tokens.AccessToken, "original long password", "replacement long password")
	}()
	// Race-instrumented Argon2id uses the generic implementation on this VPS.
	// Wait for the observed database lock rather than assuming hash timing.
	deadline := time.Now().Add(2 * time.Minute)
	for {
		var blocked bool
		if err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock')").Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("change never reached account lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = holder.Exec("UPDATE auth_sessions SET revoked_at=now() WHERE account_id=$1", a.ID); err != nil {
		t.Fatal(err)
	}
	if err = holder.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-done; !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("revoked session changed password: %v", err)
	}
}

func TestPasswordChangeAndResetSerialize(t *testing.T) {
	db, s, sessions, a, tokens := passwordChangeFixture(t)
	ctx := context.Background()
	if err := s.recovery.Request(ctx, a.Email, "https://frontend.example.invalid/auth.html"); err != nil {
		t.Fatal(err)
	}
	var id string
	var sealed []byte
	if err := db.QueryRow("SELECT id,sealed_payload FROM password_recovery_outbox WHERE account_id=$1 AND kind='reset'", a.ID).Scan(&id, &sealed); err != nil {
		t.Fatal(err)
	}
	message, err := s.recovery.OpenEmail(a.ID, id, sealed)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(message.ResetURL)
	reset := strings.TrimPrefix(u.Fragment, "reset_token=")
	results := make(chan error, 2)
	go func() {
		results <- s.Change(ctx, tokens.AccessToken, "original long password", "change replacement password")
	}()
	go func() { results <- s.recovery.Reset(ctx, reset, "reset replacement password") }()
	wins := 0
	for range 2 {
		err := <-results
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrInvalidSession) && !errors.Is(err, ErrCurrentPasswordInvalid) && !errors.Is(err, ErrInvalidResetToken) {
			t.Fatal(err)
		}
	}
	if wins != 1 {
		t.Fatal("reset/change race winners", wins)
	}
	if _, err = sessions.Authenticate(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatal("race preserved old session", err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM password_recovery_outbox WHERE kind='changed'").Scan(&count); err != nil || count != 1 {
		t.Fatal("race notice count", count, err)
	}
}
