package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

const changePasswordPath = "/api/v1/auth/password/change"
const originalChangePassword = "original synthetic password"
const replacementChangePassword = "replacement synthetic password"

func passwordChangeAccount(t *testing.T, f *churchHTTPFixture) (string, auth.SessionTokens) {
	t.Helper()
	id, _ := f.account(t, "", false)
	hash, err := auth.NewPasswordHasher().Hash(originalChangePassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec("UPDATE accounts SET password_hash=$2 WHERE id=$1", id, hash); err != nil {
		t.Fatal(err)
	}
	tokens, err := f.sessions.Login(context.Background(), accountEmail(t, f, id), originalChangePassword)
	if err != nil {
		t.Fatal(err)
	}
	return id, tokens
}
func accountEmail(t *testing.T, f *churchHTTPFixture, id string) string {
	t.Helper()
	var email string
	if err := f.db.QueryRow("SELECT email FROM accounts WHERE id=$1", id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}
func passwordChangeBody(current, next string) string {
	raw, _ := json.Marshal(map[string]string{"currentPassword": current, "newPassword": next})
	return string(raw)
}
func TestPasswordChangeThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	id, tokens := passwordChangeAccount(t, f)
	other, err := f.sessions.Login(context.Background(), accountEmail(t, f, id), originalChangePassword)
	if err != nil {
		t.Fatal(err)
	}
	w := f.request("POST", changePasswordPath, tokens.AccessToken, passwordChangeBody("wrong", replacementChangePassword))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "current_password_invalid") {
		t.Fatalf("wrong password: %d %s", w.Code, w.Body.String())
	}
	if _, err = f.sessions.Authenticate(context.Background(), tokens.AccessToken); err != nil {
		t.Fatal("typo revoked session", err)
	}
	w = f.request("POST", changePasswordPath, tokens.AccessToken, passwordChangeBody(originalChangePassword, replacementChangePassword))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"password_changed"`) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != refreshCookieName || cookies[0].MaxAge != -1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" {
		t.Fatal("refresh cookie not securely cleared", cookies)
	}
	for _, session := range []auth.SessionTokens{tokens, other} {
		if got := f.request("GET", "/api/v1/me", session.AccessToken, ""); got.Code != 401 {
			t.Fatal("access survived change", got.Code)
		}
		if _, err = f.sessions.Refresh(context.Background(), session.RefreshToken); err == nil {
			t.Fatal("refresh survived change")
		}
	}
	if _, err = f.sessions.Login(context.Background(), accountEmail(t, f, id), originalChangePassword); err == nil {
		t.Fatal("old password still works")
	}
	if _, err = f.sessions.Login(context.Background(), accountEmail(t, f, id), replacementChangePassword); err != nil {
		t.Fatal("new password rejected", err)
	}
	if strings.Contains(w.Body.String(), originalChangePassword) || strings.Contains(w.Body.String(), replacementChangePassword) {
		t.Fatal("password leaked")
	}
}
func TestPasswordChangeHTTPBoundaries(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, tokens := passwordChangeAccount(t, f)
	body := passwordChangeBody(originalChangePassword, replacementChangePassword)
	for _, tc := range []struct {
		name, token, body, origin, path string
		status                          int
	}{
		{"anonymous", "", body, f.origin, changePasswordPath, 401},
		{"bad token", "invalid", body, f.origin, changePasswordPath, 401},
		{"missing origin", tokens.AccessToken, body, "", changePasswordPath, 403},
		{"foreign origin", tokens.AccessToken, body, "https://evil.example.invalid", changePasswordPath, 403},
		{"unknown field", tokens.AccessToken, `{"currentPassword":"x","newPassword":"x","accountId":"other"}`, f.origin, changePasswordPath, 400},
		{"two objects", tokens.AccessToken, body + body, f.origin, changePasswordPath, 400},
		{"short new", tokens.AccessToken, passwordChangeBody(originalChangePassword, "short"), f.origin, changePasswordPath, 422},
		{"query", tokens.AccessToken, body, f.origin, changePasswordPath + "?accountId=other", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Authorization", "Bearer "+tc.token)
			r.RemoteAddr = "192.0.2.77:2000"
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d want%d %s", w.Code, tc.status, w.Body.String())
			}
		})
	}
	_, google := f.account(t, "", false)
	if w := f.request("POST", changePasswordPath, google, body); w.Code != 409 || !strings.Contains(w.Body.String(), "local_password_required") {
		t.Fatalf("Google-only: %d %s", w.Code, w.Body.String())
	}
}
func TestPasswordChangeAccountAndIPLimits(t *testing.T) {
	for _, scope := range []string{"account", "ip"} {
		t.Run(scope, func(t *testing.T) {
			f := newChurchHTTPFixture(t)
			_, tokens := passwordChangeAccount(t, f)
			_, google := f.account(t, "", false)
			for i := 0; i < 6; i++ {
				token := tokens.AccessToken
				remote := "192.0.2.77:2000"
				if scope == "account" {
					remote = []string{"192.0.2.1:1", "192.0.2.2:1", "192.0.2.3:1", "192.0.2.4:1", "192.0.2.5:1", "192.0.2.6:1"}[i]
				} else if i%2 == 1 {
					token = google
				}
				r := httptest.NewRequest("POST", changePasswordPath, strings.NewReader(passwordChangeBody("wrong", replacementChangePassword)))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", f.origin)
				r.Header.Set("Authorization", "Bearer "+token)
				r.RemoteAddr = remote
				w := httptest.NewRecorder()
				f.handler.ServeHTTP(w, r)
				if i < 5 && w.Code == 429 {
					t.Fatal("premature limit", i)
				}
				if i == 5 && (w.Code != 429 || w.Header().Get("Retry-After") == "") {
					t.Fatalf("%s limit bypass: %d %s", scope, w.Code, w.Body.String())
				}
			}
		})
	}
}
func TestPasswordChangeRequiresConfiguration(t *testing.T) {
	h := New(nil, Config{})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, changePasswordPath, strings.NewReader(`{}`))
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPasswordChangeRejectsMalformedUnicodeThroughGlobalApp(t *testing.T) {
	f := newChurchHTTPFixture(t)
	_, token := f.account(t, "", false)
	bodies := []string{
		`{"currentPassword":"original password","newPassword":"replacement ` + string([]byte{0xff}) + ` password"}`,
		`{"currentPassword":"original password","newPassword":"replacement \ud800 password"}`,
		`{"currentPassword":"original \udfff password","newPassword":"replacement valid password"}`,
	}
	for _, body := range bodies {
		w := f.request("POST", changePasswordPath, token, body)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_json") {
			t.Fatalf("malformed Unicode transformed instead of rejected: %d %s", w.Code, w.Body.String())
		}
	}
}
