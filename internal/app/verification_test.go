package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func authRequest(t *testing.T, app http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	return w
}

func verificationEmailForTest(t *testing.T, db *sql.DB, key []byte, email string) auth.VerificationEmail {
	t.Helper()
	var accountID, challengeID string
	var ciphertext []byte
	if err := db.QueryRow(`SELECT o.account_id,o.challenge_id,o.sealed_payload FROM verification_email_outbox o
 JOIN accounts a ON a.id=o.account_id WHERE a.email=$1`, email).Scan(&accountID, &challengeID, &ciphertext); err != nil {
		t.Fatal(err)
	}
	service, err := auth.NewVerificationService(db, auth.VerificationConfig{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	message, err := service.OpenEmail(accountID, challengeID, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func TestVerificationThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	key := bytes.Repeat([]byte{42}, 32)
	app := New(db, Config{RegistrationEnabled: true, VerificationKey: key, Now: func() time.Time { return now }})
	signup := `{"firstName":"Ada","lastName":"Member","email":"otp@example.com","password":"a sufficiently long password"}`
	w := authRequest(t, app, "register", signup)
	if w.Code != 201 {
		t.Fatalf("signup status=%d body=%s", w.Code, w.Body.String())
	}
	message := verificationEmailForTest(t, db, key, "otp@example.com")
	if strings.Contains(w.Body.String(), message.Code) {
		t.Fatal("signup leaked OTP")
	}
	var queued int
	if err := db.QueryRow("SELECT count(*) FROM verification_email_outbox").Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("queued=%d err=%v", queued, err)
	}
	// Cooldown and unknown identities deliberately have the same public response.
	cooldown := authRequest(t, app, "verification/resend", `{"email":"otp@example.com"}`)
	missing := authRequest(t, app, "verification/resend", `{"email":"missing@example.com"}`)
	if cooldown.Code != 202 || missing.Code != 202 || cooldown.Body.String() != missing.Body.String() {
		t.Fatal("resend leaks account existence or cooldown state")
	}
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"malformed", "{", 400},
		{"null", "null", 400},
		{"unknown field", `{"email":"otp@example.com","code":"wrong","admin":true}`, 400},
		{"trailing JSON", `{} {}`, 400},
		{"oversized", strings.Repeat(" ", 16385), 413},
		{"missing fields", `{}`, 422},
		{"incorrect", `{"email":"otp@example.com","code":"wrong"}`, 400},
		{"unknown account", `{"email":"missing@example.com","code":"wrong"}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := authRequest(t, app, "verification/verify", tc.body)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			var body struct {
				Error struct{ Code, RequestID string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error.Code == "" || body.Error.RequestID != w.Header().Get("X-Request-Id") {
				t.Fatalf("invalid envelope: %s", w.Body.String())
			}
		})
	}
	now = now.Add(time.Minute)
	resent := authRequest(t, app, "verification/resend", `{"email":"otp@example.com"}`)
	if resent.Code != 202 {
		t.Fatalf("resend=%d", resent.Code)
	}
	replacement := verificationEmailForTest(t, db, key, "otp@example.com")
	if replacement.Code == message.Code {
		t.Fatal("OTP reused")
	}
	oldBody, _ := json.Marshal(map[string]string{"email": "otp@example.com", "code": message.Code})
	old := authRequest(t, app, "verification/verify", string(oldBody))
	if old.Code != 400 {
		t.Fatalf("old OTP accepted: %d", old.Code)
	}
	body, _ := json.Marshal(map[string]string{"email": " OTP@EXAMPLE.COM ", "code": replacement.Code})
	verified := authRequest(t, app, "verification/verify", string(body))
	if verified.Code != 200 {
		t.Fatalf("verify=%d body=%s", verified.Code, verified.Body.String())
	}
	if !strings.Contains(verified.Body.String(), `"status":"email_verified"`) || strings.Contains(verified.Body.String(), replacement.Code) || strings.Contains(verified.Body.String(), "accessToken") || verified.Header().Get("Set-Cookie") != "" {
		t.Fatal("unexpected verification result")
	}
	replay := authRequest(t, app, "verification/verify", string(body))
	if replay.Code != 400 {
		t.Fatalf("OTP replay accepted: %d", replay.Code)
	}
	already := authRequest(t, app, "verification/resend", `{"email":"otp@example.com"}`)
	if already.Code != missing.Code || already.Body.String() != missing.Body.String() {
		t.Fatal("resend reveals verified account")
	}
	if err := db.QueryRow("SELECT count(*) FROM verification_email_outbox").Scan(&queued); err != nil || queued != 0 {
		t.Fatal("consumed mail remained queued")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"verification/verify", "verification/resend"} {
		failed := authRequest(t, app, path, `{"email":"otp@example.com","code":"12345678"}`)
		// Resend uses its own request schema, so omit the code for that endpoint.
		if path == "verification/resend" {
			failed = authRequest(t, app, path, `{"email":"otp@example.com"}`)
		}
		if failed.Code != 503 || strings.Contains(failed.Body.String(), "sql:") {
			t.Fatalf("unsafe failure %s: %d %s", path, failed.Code, failed.Body.String())
		}
	}
}

func TestAuthFailsClosedWithoutVerificationSecret(t *testing.T) {
	db := testpg.New(t)
	app := New(db, Config{RegistrationEnabled: true})
	for _, path := range []string{"register", "verification/verify", "verification/resend"} {
		w := authRequest(t, app, path, `{}`)
		if w.Code != 503 {
			t.Fatalf("%s status=%d", path, w.Code)
		}
	}
}
