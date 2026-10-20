package mailresend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

func TestRecoveryEmails(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct{ Subject, Text, Html string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Header.Get("Idempotency-Key") != "password-recovery/b8ac53d9-6b94-44ba-b173-3d665c14c631" {
			t.Error("missing idempotency")
		}
		if !strings.Contains(body.Html, "#542361") || !strings.Contains(body.Html, "viewport") {
			t.Error("missing purple mobile template")
		}
		if calls == 1 {
			if !strings.Contains(body.Subject, "Reset") || !strings.Contains(body.Text, "#reset_token=") || !strings.Contains(body.Html, "Reset password") {
				t.Error("reset link missing")
			}
		} else if !strings.Contains(body.Subject, "changed") || strings.Contains(body.Html, "reset_token=") {
			t.Error("invalid change notification")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"sent"}`))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	s, _ := newSender("test-key", "City4Christ <noreply@example.invalid>", &http.Client{Transport: redirectTransport{target: target}})
	m := auth.RecoveryEmail{Email: "test@example.invalid", ResetURL: "https://frontend.example.invalid/auth.html#reset_token=" + strings.Repeat("A", 43), ExpiresAt: time.Now().Add(15 * time.Minute)}
	id := "b8ac53d9-6b94-44ba-b173-3d665c14c631"
	if err := s.SendRecovery(context.Background(), id, m); err != nil {
		t.Fatal(err)
	}
	m.ResetURL = ""
	m.PasswordChanged = true
	if err := s.SendRecovery(context.Background(), id, m); err != nil {
		t.Fatal(err)
	}
	m.PasswordChanged = false
	m.ResetURL = "http://evil.example/auth.html#reset_token=bad"
	if err := s.SendRecovery(context.Background(), id, m); err == nil || calls != 2 {
		t.Fatal("invalid payload sent")
	}
}
