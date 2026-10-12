package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestRegistrationThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	payload := `{"firstName":"Ada","lastName":"Member","email":"Ada@example.com","password":"a sufficiently long password"}`
	request := func(handler http.Handler, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	closed := request(New(db, Config{}), payload)
	if closed.Code != http.StatusForbidden {
		t.Fatalf("signup must default closed: %d", closed.Code)
	}
	app := New(db, Config{RegistrationEnabled: true, VerificationKey: bytes.Repeat([]byte{42}, 32)})
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"invalid JSON", "{", 400},
		{"null JSON", "null", 400},
		{"unknown field", `{"admin":true}`, 400},
		{"trailing JSON", payload + ` {}`, 400},
		{"oversized body", strings.Repeat(" ", 16385), 413},
		{"validation", `{"email":"bad"}`, 422},
		{"valid", payload, 201},
		{"duplicate", payload, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(app, tc.body)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			if w.Header().Get("X-Request-Id") == "" {
				t.Fatal("missing correlation ID")
			}
			if strings.Contains(w.Body.String(), "password") && tc.status == 201 {
				t.Fatal("password field leaked")
			}
			if strings.Contains(w.Body.String(), "$argon2") {
				t.Fatal("hash leaked")
			}
			if tc.status == 201 {
				var response struct {
					Data struct{ ID, Email, Status string }
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.ID == "" || response.Data.Email != "ada@example.com" || response.Data.Status != "email_unverified" {
					t.Fatalf("unexpected response: %s", w.Body.String())
				}
				if w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String(), "accessToken") {
					t.Fatal("unverified signup must not establish a member session")
				}
			} else {
				var response struct {
					Error struct {
						Code, Message, RequestID string
						Fields                   map[string]string
					}
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code == "" || response.Error.RequestID != w.Header().Get("X-Request-Id") {
					t.Fatalf("invalid error envelope: %s", w.Body.String())
				}
			}
		})
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	failed := request(app, payload)
	if failed.Code != 500 || strings.Contains(failed.Body.String(), "sql:") {
		t.Fatalf("unsafe dependency failure: %d %s", failed.Code, failed.Body.String())
	}
}
