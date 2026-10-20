package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestPasswordRecoveryThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{42}, 32)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	config := Config{VerificationKey: key, AppOrigin: "https://api.example.invalid", AllowedOrigins: []string{"https://frontend.example.invalid"}, Now: func() time.Time { return now }}
	h := New(db, config)
	post := func(path, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/auth/"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	hash, _ := auth.NewPasswordHasher().Hash("original long password")
	a, err := auth.NewAccountRepository(db).Create(context.Background(), auth.NewAccount{Email: "recovery@example.invalid", FirstName: "Test", LastName: "User", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE accounts SET email_verified_at=$2 WHERE id=$1", a.ID, now); err != nil {
		t.Fatal(err)
	}
	request := `{"email":"recovery@example.invalid","returnTo":"https://frontend.example.invalid/auth.html"}`
	w := post("password/forgot", request, config.AppOrigin)
	if w.Code != 202 {
		t.Fatalf("forgot status=%d body=%s", w.Code, w.Body.String())
	}
	unknown := post("password/forgot", strings.Replace(request, "recovery@", "unknown@", 1), config.AppOrigin)
	cooldown := post("password/forgot", request, config.AppOrigin)
	if unknown.Code != 202 || w.Body.String() != unknown.Body.String() || w.Body.String() != cooldown.Body.String() {
		t.Fatal("account existence/cooldown leaked")
	}
	for _, target := range []string{"https://evil.example/auth.html", "https://frontend.example.invalid.evil.example/auth.html", "https://frontend.example.invalid/auth.html?x=1", "https://frontend.example.invalid/auth.html#x", "https://frontend.example.invalid/profile.html"} {
		if got := post("password/forgot", strings.Replace(request, "https://frontend.example.invalid/auth.html", target, 1), config.AppOrigin); got.Code != 422 {
			t.Fatalf("unsafe target accepted: %s (%d)", target, got.Code)
		}
	}
	if got := post("password/forgot", request, "https://evil.example"); got.Code != 403 {
		t.Fatal("CSRF accepted")
	}
	if got := post("password/forgot", `{"email":"bad","returnTo":"https://frontend.example.invalid/auth.html"}`, config.AppOrigin); got.Code != 422 {
		t.Fatal("invalid email accepted")
	}
	if got := post("password/reset", `{"token":"bad","password":"replacement long password","admin":true}`, config.AppOrigin); got.Code != 400 {
		t.Fatal("unknown JSON field accepted")
	}
	var id string
	var sealed []byte
	if err = db.QueryRow("SELECT id,sealed_payload FROM password_recovery_outbox WHERE account_id=$1", a.ID).Scan(&id, &sealed); err != nil {
		t.Fatal(err)
	}
	v, _ := auth.NewVerificationService(db, auth.VerificationConfig{Key: key})
	s := auth.NewPasswordRecoveryService(db, v)
	message, err := s.OpenEmail(a.ID, id, sealed)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(message.ResetURL)
	token := strings.TrimPrefix(u.Fragment, "reset_token=")
	body, _ := json.Marshal(map[string]string{"token": token, "password": "replacement long password"})
	w = post("password/reset", string(body), config.AppOrigin)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), token) {
		t.Fatalf("reset status=%d body=%s", w.Code, w.Body.String())
	}
	if got := post("password/reset", string(body), config.AppOrigin); got.Code != 400 {
		t.Fatal("replay accepted")
	}
	for i := 0; i < 6; i++ {
		got := post("password/forgot", `{"email":"limited@example.invalid","returnTo":"https://frontend.example.invalid/auth.html"}`, config.AppOrigin)
		if i == 5 && got.Code != 429 {
			t.Fatalf("account limit missing: %d", got.Code)
		}
	}
	// A distinct client IP cannot bypass the per-account reset-confirm limit.
	for i := 0; i < 11; i++ {
		got := post("password/reset", `{"token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","password":"replacement long password"}`, config.AppOrigin)
		if i == 10 && got.Code != 429 {
			t.Fatalf("confirmation limit missing: %d", got.Code)
		}
	}
}

func TestPasswordRecoveryRequiresConfiguredOrigin(t *testing.T) {
	h := http.NewServeMux()
	passwordRecoveryRoutes(h, auth.NewPasswordRecoveryService(nil, nil), nil, Config{})
	for _, path := range []string{"password/forgot", "password/reset"} {
		w := authRequest(t, h, path, `{}`)
		if w.Code != 503 {
			t.Fatalf("recovery enabled without APP_ORIGIN: %s status=%d", path, w.Code)
		}
	}
}
