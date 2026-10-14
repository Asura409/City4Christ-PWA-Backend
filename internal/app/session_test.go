package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

func TestSessionRoutesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	ctx := context.Background()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	origin := "https://api.example.invalid"
	frontend := "https://zeetechdot.github.io"
	h := New(db, Config{RegistrationEnabled: true, VerificationKey: bytes.Repeat([]byte{8}, 32), Now: func() time.Time { return now }, AppOrigin: origin, AllowedOrigins: []string{frontend}, CrossSiteCookies: true})
	request := func(method, path, body, access string, cookie *http.Cookie, requestOrigin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.1:1234"
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if access != "" {
			r.Header.Set("Authorization", "Bearer "+access)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if requestOrigin != "" {
			r.Header.Set("Origin", requestOrigin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	signup := `{"firstName":"Session","lastName":"Tester","email":"http@example.invalid","password":"a sufficiently long password"}`
	if w := request("POST", "/api/v1/auth/register", signup, "", nil, origin); w.Code != 201 {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	code := verificationEmailForTest(t, db, bytes.Repeat([]byte{8}, 32), "http@example.invalid").Code
	if w := request("POST", "/api/v1/auth/verification/verify", `{"email":"http@example.invalid","code":"`+code+`"}`, "", nil, origin); w.Code != 200 {
		t.Fatal("verification failed")
	}
	loginBody := `{"email":"http@example.invalid","password":"a sufficiently long password"}`
	w := request("POST", "/api/v1/auth/login", loginBody, "", nil, frontend)
	if w.Code != 200 {
		t.Fatalf("login %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			AccessToken string
			ExpiresIn   int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Data.AccessToken == "" || body.Data.ExpiresIn != 600 {
		t.Fatalf("access response: %s", w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if cookie.Name != "__Host-c4c_refresh" || !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteNoneMode || cookie.MaxAge <= 0 {
		t.Fatalf("unsafe cookie %+v", cookie)
	}
	if strings.Contains(w.Body.String(), cookie.Value) {
		t.Fatal("refresh leaked in JSON")
	}
	if w.Header().Get("Access-Control-Allow-Origin") != frontend || w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("CORS missing")
	}
	if w := request("GET", "/api/v1/me", "", body.Data.AccessToken, nil, frontend); w.Code != 200 || strings.Contains(w.Body.String(), "password") {
		t.Fatalf("me %d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/api/v1/me?access_token="+body.Data.AccessToken, "", "", cookie, frontend); w.Code != 401 {
		t.Fatal("accepted access outside Authorization")
	}
	if w := request("POST", "/api/v1/auth/refresh", `{}`, "", cookie, "https://evil.example"); w.Code != 403 {
		t.Fatal("refresh CSRF accepted")
	}
	if w := request("POST", "/api/v1/auth/refresh", `{}`, "", cookie, ""); w.Code != 403 {
		t.Fatal("missing Origin accepted")
	}
	refreshed := request("POST", "/api/v1/auth/refresh", `{}`, "", cookie, frontend)
	if refreshed.Code != 200 {
		t.Fatalf("refresh %d %s", refreshed.Code, refreshed.Body.String())
	}
	next := refreshed.Result().Cookies()[0]
	if next.Value == cookie.Value {
		t.Fatal("no rotation")
	}
	if w := request("POST", "/api/v1/auth/refresh", `{}`, "", cookie, frontend); w.Code != 401 {
		t.Fatal("refresh replay accepted")
	}
	if w := request("POST", "/api/v1/auth/refresh", `{}`, "", next, frontend); w.Code != 401 {
		t.Fatal("refresh family not revoked")
	}
	logged := request("POST", "/api/v1/auth/login", loginBody, "", nil, origin)
	if logged.Code != 200 {
		t.Fatal("relogin failed")
	}
	if err := json.Unmarshal(logged.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	logout := request("POST", "/api/v1/auth/logout", `{}`, "", logged.Result().Cookies()[0], origin)
	if logout.Code != 200 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	if w := request("GET", "/api/v1/me", "", body.Data.AccessToken, nil, origin); w.Code != 401 {
		t.Fatal("logout access still usable")
	}
	if w := request("OPTIONS", "/api/v1/auth/login", "", "", nil, frontend); w.Code != 204 {
		t.Fatalf("preflight %d", w.Code)
	}
}

func TestAccountAndIPHTTPRateLimits(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	h := New(db, Config{VerificationKey: make([]byte, 32), AppOrigin: "https://api.example.invalid", Now: func() time.Time { return now }})
	call := func(ip, email string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"`+email+`","password":"wrong"}`))
		r.RemoteAddr = ip + ":1234"
		r.Header.Set("Origin", "https://api.example.invalid")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for i := range 10 {
		if w := call("192.0.2.1", "rate@example.invalid"); w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	if w := call("192.0.2.2", " RATE@EXAMPLE.INVALID "); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("account limit %d", w.Code)
	}
	// Malformed requests still count toward IP limit; spoofed forwarding headers are ignored.
	for i := range 61 {
		r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader("{"))
		r.RemoteAddr = "198.51.100.1:1234"
		r.Header.Set("Origin", "https://api.example.invalid")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", "203.0.113."+string(rune('a'+i)))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if i == 60 && w.Code != 429 {
			t.Fatalf("IP limit %d", w.Code)
		}
	}
}

func TestTrustedProxyChainAndCookieDefaults(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.99, 198.51.100.10, 127.0.0.2")
	if got := clientIP(r, Config{}); got != "127.0.0.1" {
		t.Fatalf("untrusted proxy bypass %s", got)
	}
	if got := clientIP(r, Config{TrustedProxyCIDRs: []string{"127.0.0.0/8"}}); got != "198.51.100.10" {
		t.Fatalf("forward chain %s", got)
	}
	w := httptest.NewRecorder()
	setRefresh(w, Config{}, "test", time.Now().Add(time.Hour))
	if w.Result().Cookies()[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("default cookie policy weakened")
	}
}
