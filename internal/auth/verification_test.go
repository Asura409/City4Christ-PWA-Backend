package auth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestVerificationConfiguration(t *testing.T) {
	for _, key := range [][]byte{nil, make([]byte, 16), make([]byte, 31), make([]byte, 33)} {
		if _, err := NewVerificationService(nil, VerificationConfig{Key: key}); err == nil {
			t.Fatalf("accepted %d-byte key", len(key))
		}
	}
}

func TestEmailVerification(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	key := bytes.Repeat([]byte{42}, 32)
	verification, err := NewVerificationService(db, VerificationConfig{Key: key, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	registration := NewRegistrationService(NewPasswordHasher(), verification)
	create := func(t *testing.T, email string) (Account, VerificationEmail, string) {
		t.Helper()
		a, err := registration.Register(ctx, Registration{FirstName: "Ada", LastName: "Member", Email: email, Password: "a sufficiently long password"})
		if err != nil {
			t.Fatal(err)
		}
		message, id, _ := queuedVerification(t, db, verification, a.ID)
		return a, message, id
	}
	t.Run("atomic issuance and protected storage", func(t *testing.T) {
		a, message, id := create(t, "issue@example.com")
		if message.Email != a.Email || !regexp.MustCompile(`^[0-9]{8}$`).MatchString(message.Code) || !message.ExpiresAt.Equal(now.Add(10*time.Minute)) {
			t.Fatalf("bad message metadata for account %s", a.ID)
		}
		_, _, ciphertext := queuedVerification(t, db, verification, a.ID)
		if bytes.Contains(ciphertext, []byte(message.Code)) || bytes.Contains(ciphertext, []byte(a.Email)) {
			t.Fatal("outbox contains plaintext secrets")
		}
		var digest []byte
		if err := db.QueryRowContext(ctx, "SELECT code_digest FROM email_verifications WHERE account_id=$1", a.ID).Scan(&digest); err != nil {
			t.Fatal(err)
		}
		if len(digest) != 32 || bytes.Equal(digest, []byte(message.Code)) {
			t.Fatal("OTP verifier not protected")
		}
		if _, err := verification.OpenEmail(a.ID, "another-challenge", ciphertext); err == nil {
			t.Fatal("ciphertext accepted under wrong challenge")
		}
		ciphertext[len(ciphertext)-1] ^= 1
		if _, err := verification.OpenEmail(a.ID, id, ciphertext); err == nil {
			t.Fatal("tampered email accepted")
		}
	})
	t.Run("single use and account binding", func(t *testing.T) {
		a, message, _ := create(t, "once@example.com")
		if err := verification.Verify(ctx, "other@example.com", message.Code); !errors.Is(err, ErrInvalidOTP) {
			t.Fatalf("cross-account verification: %v", err)
		}
		if err := verification.Verify(ctx, " ONCE@EXAMPLE.COM ", message.Code); err != nil {
			t.Fatal(err)
		}
		stored, err := NewAccountRepository(db).ByEmail(ctx, a.Email)
		if err != nil || stored.EmailVerifiedAt == nil || !stored.EmailVerifiedAt.Equal(now) {
			t.Fatalf("verification not persisted: %v", err)
		}
		if err := verification.Verify(ctx, a.Email, message.Code); !errors.Is(err, ErrInvalidOTP) {
			t.Fatalf("replay: %v", err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM verification_email_outbox WHERE account_id=$1", a.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("consumed email retained: %d %v", count, err)
		}
		var digest []byte
		if err := db.QueryRowContext(ctx, "SELECT code_digest FROM email_verifications WHERE account_id=$1", a.ID).Scan(&digest); err != nil || digest != nil {
			t.Fatal("consumed verifier retained")
		}
	})
	t.Run("expiry at exact boundary", func(t *testing.T) {
		a, message, _ := create(t, "expires@example.com")
		now = now.Add(10 * time.Minute)
		if err := verification.Verify(ctx, a.Email, message.Code); !errors.Is(err, ErrInvalidOTP) {
			t.Fatalf("expired code: %v", err)
		}
	})
	t.Run("attempt limit", func(t *testing.T) {
		a, message, _ := create(t, "attempts@example.com")
		for range 5 {
			if err := verification.Verify(ctx, a.Email, "wrong"); !errors.Is(err, ErrInvalidOTP) {
				t.Fatalf("incorrect code: %v", err)
			}
		}
		if err := verification.Verify(ctx, a.Email, message.Code); !errors.Is(err, ErrInvalidOTP) {
			t.Fatalf("exhausted code accepted: %v", err)
		}
		var attempts int
		if err := db.QueryRowContext(ctx, "SELECT attempts FROM email_verifications WHERE account_id=$1", a.ID).Scan(&attempts); err != nil || attempts != 5 {
			t.Fatalf("attempts=%d err=%v", attempts, err)
		}
	})
	t.Run("last allowed attempt can succeed", func(t *testing.T) {
		a, message, _ := create(t, "last@example.com")
		for range 4 {
			if err := verification.Verify(ctx, a.Email, "wrong"); !errors.Is(err, ErrInvalidOTP) {
				t.Fatal(err)
			}
		}
		if err := verification.Verify(ctx, a.Email, message.Code); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("resend cooldown and invalidation", func(t *testing.T) {
		a, old, oldID := create(t, "resend@example.com")
		if err := verification.Resend(ctx, a.Email); !errors.Is(err, ErrResendCooldown) {
			t.Fatalf("cooldown: %v", err)
		}
		now = now.Add(time.Minute - time.Nanosecond)
		if err := verification.Resend(ctx, a.Email); !errors.Is(err, ErrResendCooldown) {
			t.Fatalf("early resend: %v", err)
		}
		now = now.Add(time.Nanosecond)
		if err := verification.Resend(ctx, a.Email); err != nil {
			t.Fatal(err)
		}
		fresh, freshID, _ := queuedVerification(t, db, verification, a.ID)
		if fresh.Code == old.Code || freshID == oldID {
			t.Fatal("resend reused previous credential")
		}
		if err := verification.Verify(ctx, a.Email, old.Code); !errors.Is(err, ErrInvalidOTP) {
			t.Fatalf("old code: %v", err)
		}
		if err := verification.Verify(ctx, a.Email, fresh.Code); err != nil {
			t.Fatal(err)
		}
		if err := verification.Resend(ctx, a.Email); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM verification_email_outbox WHERE account_id=$1", a.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("verified account received mail")
		}
	})
	t.Run("concurrent verification has exactly one winner", func(t *testing.T) {
		a, message, _ := create(t, "race@example.com")
		var wg sync.WaitGroup
		results := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() { defer wg.Done(); results <- verification.Verify(ctx, a.Email, message.Code) }()
		}
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !errors.Is(err, ErrInvalidOTP) {
				t.Fatal(err)
			}
		}
		if successes != 1 {
			t.Fatalf("successful uses=%d", successes)
		}
	})
	t.Run("concurrent invalid attempts cannot exceed limit", func(t *testing.T) {
		a, message, _ := create(t, "race-attempts@example.com")
		var wg sync.WaitGroup
		results := make(chan error, 12)
		for range 12 {
			wg.Add(1)
			go func() { defer wg.Done(); results <- verification.Verify(ctx, a.Email, "wrong") }()
		}
		wg.Wait()
		close(results)
		for err := range results {
			if !errors.Is(err, ErrInvalidOTP) {
				t.Fatal(err)
			}
		}
		var attempts int
		if err := db.QueryRowContext(ctx, "SELECT attempts FROM email_verifications WHERE account_id=$1", a.ID).Scan(&attempts); err != nil || attempts != 5 {
			t.Fatalf("attempts=%d err=%v", attempts, err)
		}
		if err := verification.Verify(ctx, a.Email, message.Code); !errors.Is(err, ErrInvalidOTP) {
			t.Fatal("exhausted credential accepted")
		}
	})
	t.Run("concurrent resend queues one replacement", func(t *testing.T) {
		a, _, _ := create(t, "race-resend@example.com")
		now = now.Add(time.Minute)
		var wg sync.WaitGroup
		results := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() { defer wg.Done(); results <- verification.Resend(ctx, a.Email) }()
		}
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !errors.Is(err, ErrResendCooldown) {
				t.Fatal(err)
			}
		}
		if successes != 1 {
			t.Fatalf("resend successes=%d", successes)
		}
		queuedVerification(t, db, verification, a.ID) // exactly one queued email
	})
	t.Run("unknown account resend is generic", func(t *testing.T) {
		if err := verification.Resend(ctx, "missing@example.com"); err != nil {
			t.Fatal(err)
		}
	})
}

func queuedVerification(t *testing.T, db *sql.DB, service *VerificationService, accountID string) (VerificationEmail, string, []byte) {
	t.Helper()
	var id string
	var ciphertext []byte
	rows, err := db.Query("SELECT challenge_id,sealed_payload FROM verification_email_outbox WHERE account_id=$1", accountID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no verification email queued")
	}
	if err := rows.Scan(&id, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("more than one email queued")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	message, err := service.OpenEmail(accountID, id, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	return message, id, ciphertext
}

func TestRegistrationAndResendRollBackOnOutboxFailure(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	service, err := NewVerificationService(db, VerificationConfig{Key: bytes.Repeat([]byte{42}, 32), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	registration := NewRegistrationService(NewPasswordHasher(), service)
	valid := Registration{FirstName: "Ada", LastName: "Member", Email: "rollback@example.com", Password: "a sufficiently long password"}
	a, err := registration.Register(ctx, valid)
	if err != nil {
		t.Fatal(err)
	}
	original, originalID, _ := queuedVerification(t, db, service, a.ID)
	_, err = db.ExecContext(ctx, `CREATE FUNCTION reject_email() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'simulated outbox failure'; END; $$;
 CREATE TRIGGER reject_email BEFORE INSERT ON verification_email_outbox FOR EACH ROW EXECUTE FUNCTION reject_email();`)
	if err != nil {
		t.Fatal(err)
	}
	valid.Email = "failed@example.com"
	if _, err := registration.Register(ctx, valid); err == nil {
		t.Fatal("expected outbox failure")
	}
	if _, err := NewAccountRepository(db).ByEmail(ctx, valid.Email); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("partial account persisted: %v", err)
	}
	now = now.Add(time.Minute)
	if err := service.Resend(ctx, a.Email); err == nil || errors.Is(err, ErrResendCooldown) {
		t.Fatalf("expected outbox failure: %v", err)
	}
	retained, id, _ := queuedVerification(t, db, service, a.ID)
	if retained.Code != original.Code || id != originalID {
		t.Fatal("failed resend changed the existing credential")
	}
	if err := service.Verify(ctx, a.Email, original.Code); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM email_verifications").Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial challenge persisted: %d %v", count, err)
	}
}
