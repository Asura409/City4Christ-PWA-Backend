package auth

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

type emailSenderFunc func(context.Context, string, VerificationEmail) error

func (f emailSenderFunc) Send(ctx context.Context, id string, email VerificationEmail) error {
	return f(ctx, id, email)
}

func TestVerificationEmailDelivery(t *testing.T) {
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
	register := NewRegistrationService(NewPasswordHasher(), service)
	create := func(t *testing.T, email string) (Account, VerificationEmail, string) {
		t.Helper()
		account, err := register.Register(ctx, Registration{Email: email, FirstName: "Ada", LastName: "Member", Password: "a sufficiently long password"})
		if err != nil {
			t.Fatal(err)
		}
		message, id, _ := queuedVerification(t, db, service, account.ID)
		return account, message, id
	}
	queuedCount := func(t *testing.T, want int) {
		t.Helper()
		var count int
		if err := db.QueryRow("SELECT count(*) FROM verification_email_outbox").Scan(&count); err != nil || count != want {
			t.Fatalf("queued=%d want=%d err=%v", count, want, err)
		}
	}
	t.Run("accepted email is acknowledged without consuming code", func(t *testing.T) {
		account, message, id := create(t, "send@example.com")
		calls := 0
		sender := emailSenderFunc(func(ctx context.Context, gotID string, got VerificationEmail) error {
			calls++
			if gotID != id || got != message {
				t.Fatal("wrong email dispatched")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("missing delivery deadline")
			}
			return nil
		})
		processed, err := service.DeliverNext(ctx, sender)
		if err != nil || !processed || calls != 1 {
			t.Fatalf("processed=%v calls=%d err=%v", processed, calls, err)
		}
		queuedCount(t, 0)
		if processed, err := service.DeliverNext(ctx, sender); err != nil || processed || calls != 1 {
			t.Fatal("acknowledged email sent twice")
		}
		if err := service.Verify(ctx, account.Email, message.Code); err != nil {
			t.Fatal("delivery consumed the OTP")
		}
	})
	t.Run("retry preserves payload and respects backoff", func(t *testing.T) {
		account, message, id := create(t, "retry@example.com")
		calls := 0
		sender := emailSenderFunc(func(_ context.Context, gotID string, got VerificationEmail) error {
			calls++
			if gotID != id || got != message {
				t.Fatal("retry changed email")
			}
			if calls == 1 {
				return &EmailDeliveryError{Code: "provider_rate_limited", Retryable: true, RetryAfter: 7 * time.Second}
			}
			return nil
		})
		if processed, err := service.DeliverNext(ctx, sender); !processed || err == nil {
			t.Fatal("expected persisted retry failure")
		}
		var attempts int
		var next time.Time
		var code string
		if err := db.QueryRow("SELECT delivery_attempts,next_attempt_at,last_error_code FROM verification_email_outbox WHERE account_id=$1", account.ID).Scan(&attempts, &next, &code); err != nil || attempts != 1 || !next.Equal(now.Add(7*time.Second)) || code != "provider_rate_limited" {
			t.Fatalf("invalid retry state: attempts=%d next=%s code=%s err=%v", attempts, next, code, err)
		}
		if processed, err := service.DeliverNext(ctx, sender); err != nil || processed {
			t.Fatal("retried before backoff")
		}
		now = now.Add(7 * time.Second)
		if processed, err := service.DeliverNext(ctx, sender); err != nil || !processed || calls != 2 {
			t.Fatalf("retry failed: %v", err)
		}
		queuedCount(t, 0)
	})
	t.Run("backoff starts after provider failure", func(t *testing.T) {
		account, _, _ := create(t, "slow-provider@example.com")
		sender := emailSenderFunc(func(context.Context, string, VerificationEmail) error {
			now = now.Add(10 * time.Second)
			return &EmailDeliveryError{Code: "provider_unavailable", Retryable: true}
		})
		if processed, err := service.DeliverNext(ctx, sender); !processed || err == nil {
			t.Fatal("expected retry failure")
		}
		var next time.Time
		if err := db.QueryRow("SELECT next_attempt_at FROM verification_email_outbox WHERE account_id=$1", account.ID).Scan(&next); err != nil || !next.Equal(now.Add(5*time.Second)) {
			t.Fatalf("retry scheduled before backoff elapsed: next=%s now=%s err=%v", next, now, err)
		}
		now = now.Add(5 * time.Second)
		success := emailSenderFunc(func(context.Context, string, VerificationEmail) error { return nil })
		if processed, err := service.DeliverNext(ctx, success); err != nil || !processed {
			t.Fatal(err)
		}
		queuedCount(t, 0)
	})
	t.Run("permanent failures are retained safely then expire", func(t *testing.T) {
		account, _, _ := create(t, "rejected@example.com")
		calls := 0
		sender := emailSenderFunc(func(context.Context, string, VerificationEmail) error {
			calls++
			return &EmailDeliveryError{Code: "provider_rejected"}
		})
		if processed, err := service.DeliverNext(ctx, sender); !processed || err == nil {
			t.Fatal("expected permanent failure")
		}
		var failed *time.Time
		if err := db.QueryRow("SELECT failed_at FROM verification_email_outbox WHERE account_id=$1", account.ID).Scan(&failed); err != nil || failed == nil {
			t.Fatal("permanent failure not marked")
		}
		if processed, err := service.DeliverNext(ctx, sender); err != nil || processed || calls != 1 {
			t.Fatal("permanent failure retried")
		}
		now = now.Add(VerificationLifetime)
		if processed, err := service.DeliverNext(ctx, sender); err != nil || !processed || calls != 1 {
			t.Fatal("expired failed payload not removed")
		}
		queuedCount(t, 0)
	})
	t.Run("unknown provider errors are sanitized and retries bounded", func(t *testing.T) {
		account, _, _ := create(t, "exhausted@example.com")
		sender := emailSenderFunc(func(context.Context, string, VerificationEmail) error {
			return errors.New("sensitive OTP and provider key")
		})
		for attempt := 1; attempt <= 5; attempt++ {
			processed, err := service.DeliverNext(ctx, sender)
			var failure *EmailDeliveryError
			if !processed || !errors.As(err, &failure) || failure.Code != "provider_unavailable" {
				t.Fatalf("unsafe failure: %v", err)
			}
			now = now.Add(time.Minute)
		}
		var attempts int
		var failed *time.Time
		var code string
		if err := db.QueryRow("SELECT delivery_attempts,failed_at,last_error_code FROM verification_email_outbox WHERE account_id=$1", account.ID).Scan(&attempts, &failed, &code); err != nil || attempts != 5 || failed == nil || code != "provider_unavailable" {
			t.Fatal("retry limit or redaction failed")
		}
		if processed, err := service.DeliverNext(ctx, sender); err != nil || processed {
			t.Fatal("retried after limit")
		}
		now = now.Add(VerificationLifetime)
		if processed, err := service.DeliverNext(ctx, sender); err != nil || !processed {
			t.Fatal("expired exhausted payload not removed")
		}
		queuedCount(t, 0)
	})
	t.Run("expired code is never dispatched", func(t *testing.T) {
		create(t, "expired-mail@example.com")
		now = now.Add(VerificationLifetime)
		sender := emailSenderFunc(func(context.Context, string, VerificationEmail) error { t.Fatal("expired code sent"); return nil })
		if processed, err := service.DeliverNext(ctx, sender); err != nil || !processed {
			t.Fatalf("expiry cleanup: %v", err)
		}
		queuedCount(t, 0)
	})
	t.Run("concurrent workers dispatch once", func(t *testing.T) {
		create(t, "workers@example.com")
		var calls atomic.Int32
		sender := emailSenderFunc(func(context.Context, string, VerificationEmail) error { calls.Add(1); return nil })
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for range 8 {
			wg.Add(1)
			go func() { defer wg.Done(); _, err := service.DeliverNext(ctx, sender); errs <- err }()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if calls.Load() != 1 {
			t.Fatalf("dispatches=%d", calls.Load())
		}
		queuedCount(t, 0)
	})
	t.Run("claimed candidates are revalidated", func(t *testing.T) {
		for _, state := range []string{"already drained", "retry deferred", "permanently failed"} {
			t.Run(state, func(t *testing.T) {
				account, _, _ := create(t, "revalidate-"+strings.ReplaceAll(state, " ", "-")+"@example.com")
				switch state {
				case "already drained":
					if _, err := db.Exec(`DELETE FROM verification_email_outbox WHERE account_id=$1`, account.ID); err != nil {
						t.Fatal(err)
					}
				case "retry deferred":
					if _, err := db.Exec(`UPDATE verification_email_outbox SET next_attempt_at=$2 WHERE account_id=$1`, account.ID, now.Add(time.Minute)); err != nil {
						t.Fatal(err)
					}
				case "permanently failed":
					if _, err := db.Exec(`UPDATE verification_email_outbox SET failed_at=$2 WHERE account_id=$1`, account.ID, now); err != nil {
						t.Fatal(err)
					}
				}
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				var locked string
				if err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, account.ID).Scan(&locked); err != nil {
					t.Fatal(err)
				}
				sender := emailSenderFunc(func(context.Context, string, VerificationEmail) error {
					t.Error("stale claim dispatched email")
					return nil
				})
				if worked, err := service.deliverClaimed(ctx, tx, account.ID, sender); err != nil || worked {
					t.Fatalf("stale claim: worked=%v err=%v", worked, err)
				}
				tx.Rollback()
				if _, err = db.Exec(`DELETE FROM verification_email_outbox WHERE account_id=$1`, account.ID); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
	t.Run("unreadable outbox fails closed", func(t *testing.T) {
		account, _, _ := create(t, "corrupt@example.com")
		if _, err := db.Exec("UPDATE verification_email_outbox SET sealed_payload=$2 WHERE account_id=$1", account.ID, make([]byte, 28)); err != nil {
			t.Fatal(err)
		}
		sender := emailSenderFunc(func(context.Context, string, VerificationEmail) error {
			t.Fatal("corrupt email dispatched")
			return nil
		})
		processed, err := service.DeliverNext(ctx, sender)
		var failure *EmailDeliveryError
		if !processed || !errors.As(err, &failure) || failure.Code != "outbox_unreadable" || failure.Retryable {
			t.Fatalf("corrupt email handling: %v", err)
		}
	})
}
