package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

type recoverySenderFunc func(context.Context, string, RecoveryEmail) error

func (f recoverySenderFunc) SendRecovery(ctx context.Context, id string, m RecoveryEmail) error {
	return f(ctx, id, m)
}
func TestRecoveryDelivery(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 123456789, time.UTC)
	v, _ := NewVerificationService(db, VerificationConfig{Key: bytes.Repeat([]byte{42}, 32), Now: func() time.Time { return now }})
	s := NewPasswordRecoveryService(db, v)
	hash, _ := NewPasswordHasher().Hash("original long password")
	a, err := NewAccountRepository(db).Create(ctx, NewAccount{Email: "delivery@example.invalid", FirstName: "Test", LastName: "User", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET email_verified_at=$2 WHERE id=$1", a.ID, now); err != nil {
		t.Fatal(err)
	}
	issue := func() {
		t.Helper()
		if err := s.Request(ctx, a.Email, "https://frontend.example.invalid/auth.html"); err != nil {
			t.Fatal(err)
		}
	}
	issue()
	calls := 0
	var previousID string
	var previous RecoveryEmail
	sender := recoverySenderFunc(func(ctx context.Context, id string, m RecoveryEmail) error {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("no deadline")
		}
		if calls == 1 {
			previousID = id
			previous = m
			return &EmailDeliveryError{Code: "provider_unavailable", Retryable: true}
		}
		if id != previousID || m != previous {
			t.Fatal("retry changed message")
		}
		return nil
	})
	if processed, err := s.DeliverNext(ctx, sender); !processed || err == nil {
		t.Fatal("failure not returned")
	}
	if processed, err := s.DeliverNext(ctx, sender); processed || err != nil {
		t.Fatal("backoff ignored")
	}
	now = now.Add(5 * time.Second)
	if processed, err := s.DeliverNext(ctx, sender); !processed || err != nil || calls != 2 {
		t.Fatal("retry failed", err)
	}
	now = now.Add(time.Minute)
	issue()
	now = now.Add(15 * time.Minute)
	forbidden := recoverySenderFunc(func(context.Context, string, RecoveryEmail) error { t.Fatal("expired message sent"); return nil })
	if processed, err := s.DeliverNext(ctx, forbidden); !processed || err != nil {
		t.Fatal("expired message not discarded", err)
	}
	now = now.Add(time.Minute)
	issue()
	if _, err = db.Exec("UPDATE password_recovery_outbox SET sealed_payload='\\x0001'::bytea"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeliverNext(ctx, forbidden); err == nil {
		t.Fatal("tamper not rejected")
	} else {
		var failure *EmailDeliveryError
		if !errors.As(err, &failure) || failure.Code != "outbox_unreadable" {
			t.Fatal(err)
		}
	}
}
