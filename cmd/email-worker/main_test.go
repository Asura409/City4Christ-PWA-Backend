package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func TestEmailWorkerConfiguration(t *testing.T) {
	values := map[string]string{}
	env := func(key string) string { return values[key] }
	config, err := loadConfig(env)
	if err != nil || config.enabled {
		t.Fatal("delivery must default disabled")
	}
	values = map[string]string{"EMAIL_DELIVERY_MODE": "resend", "APP_ENV": "staging", "DATABASE_URL": "postgres://localhost/test", "VERIFICATION_KEY_BASE64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "RESEND_API_KEY": "test-key", "EMAIL_FROM": "noreply@example.com", "EMAIL_RECIPIENT_ALLOWLIST": "tester@example.com"}
	if config, err = loadConfig(env); err != nil || !config.enabled || !config.allowed("tester@example.com") || config.allowed("member@example.com") {
		t.Fatalf("staging config: %v", err)
	}
	for _, name := range []string{"APP_ENV", "DATABASE_URL", "VERIFICATION_KEY_BASE64", "RESEND_API_KEY", "EMAIL_FROM", "EMAIL_RECIPIENT_ALLOWLIST"} {
		t.Run(name, func(t *testing.T) {
			previous := values[name]
			values[name] = ""
			defer func() { values[name] = previous }()
			if _, err := loadConfig(env); err == nil {
				t.Fatalf("accepted missing %s", name)
			}
		})
	}
	values["APP_ENV"] = "production"
	values["EMAIL_RECIPIENT_ALLOWLIST"] = ""
	config, err = loadConfig(env)
	if err != nil || !config.allowed("member@example.com") {
		t.Fatalf("production config: %v", err)
	}
	values["VERIFICATION_KEY_BASE64"] = "invalid-secret-material"
	if _, err := loadConfig(env); err == nil || strings.Contains(err.Error(), "invalid-secret-material") {
		t.Fatal("secret validation failure")
	}
}

type recordingSender struct{ calls int }

func (s *recordingSender) Send(context.Context, string, auth.VerificationEmail) error {
	s.calls++
	return nil
}

func TestStagingRecipientGuard(t *testing.T) {
	config := workerConfig{environment: "staging", recipients: map[string]bool{"tester@example.com": true}}
	provider := &recordingSender{}
	sender := guardedSender{config: config, sender: provider}
	err := sender.Send(context.Background(), "challenge", auth.VerificationEmail{Email: "member@example.com"})
	var failure *auth.EmailDeliveryError
	if !errors.As(err, &failure) || failure.Retryable || strings.Contains(err.Error(), "@") || provider.calls != 0 {
		t.Fatal("staging recipient was sent to provider")
	}
	if err := sender.Send(context.Background(), "challenge", auth.VerificationEmail{Email: "tester@example.com"}); err != nil || provider.calls != 1 {
		t.Fatal("allowlisted recipient blocked")
	}
}

func TestDisabledWorkerWaitsForShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, workerConfig{}) }()
	select {
	case <-done:
		t.Fatal("disabled hosted worker exited before shutdown")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestExplicitAllRecipientPolicy(t *testing.T) {
	values := map[string]string{"EMAIL_DELIVERY_MODE": "resend", "APP_ENV": "staging", "DATABASE_URL": "postgres://localhost/test", "VERIFICATION_KEY_BASE64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))), "RESEND_API_KEY": "test-key", "EMAIL_FROM": "noreply@example.com", "EMAIL_RECIPIENT_ALLOWLIST": " * "}
	config, err := loadConfig(func(k string) string { return values[k] })
	if err != nil {
		t.Fatalf("explicit all-recipient policy rejected: %v", err)
	}
	provider := &recordingSender{}
	sender := guardedSender{config: config, sender: provider}
	for _, email := range []string{"first@example.com", "second@example.org"} {
		if err = sender.Send(context.Background(), "test-challenge", auth.VerificationEmail{Email: email}); err != nil {
			t.Fatal(err)
		}
	}
	if provider.calls != 2 {
		t.Fatal("open beta recipients blocked")
	}
	values["EMAIL_RECIPIENT_ALLOWLIST"] = "*,tester@example.com"
	if _, err = loadConfig(func(k string) string { return values[k] }); err == nil {
		t.Fatal("ambiguous mixed wildcard policy accepted")
	}
}
