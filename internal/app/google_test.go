package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/database"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/testpg"
)

type routeGoogleProvider struct{ nonce, verifier string }

func (p *routeGoogleProvider) AuthURL(state, nonce, verifier string) string {
	p.nonce, p.verifier = nonce, verifier
	return "https://accounts.google.com/auth?state=" + state
}
func (p *routeGoogleProvider) Exchange(_ context.Context, code, nonce, verifier string) (auth.GoogleIdentity, error) {
	if code != "code" || nonce != p.nonce || verifier != p.verifier {
		return auth.GoogleIdentity{}, auth.ErrInvalidOIDC
	}
	return auth.GoogleIdentity{Subject: "route-subject", Email: "route-google@example.invalid", EmailVerified: true}, nil
}
func TestGoogleRoutesThroughGlobalApp(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	origin := "https://api.example.invalid"
	h := New(db, Config{RegistrationEnabled: true, VerificationKey: make([]byte, 32), AppOrigin: origin, GoogleProvider: &routeGoogleProvider{}})
	start := httptest.NewRecorder()
	h.ServeHTTP(start, httptest.NewRequest("GET", "/api/v1/auth/google/start", nil))
	if start.Code != 200 {
		t.Fatalf("start %d %s", start.Code, start.Body.String())
	}
	var result struct{ Data struct{ URL string } }
	if err := json.Unmarshal(start.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(result.Data.URL)
	if err != nil {
		t.Fatal(err)
	}
	cookie := start.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe OAuth cookie")
	}
	path := "/api/v1/auth/google/callback?state=" + u.Query().Get("state") + "&code=code"
	missing := httptest.NewRecorder()
	h.ServeHTTP(missing, httptest.NewRequest("GET", path, nil))
	if missing.Code != 400 {
		t.Fatalf("callback no binding %d", missing.Code)
	}
	r := httptest.NewRequest("GET", path, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "accessToken") {
		t.Fatalf("callback %d %s", w.Code, w.Body.String())
	}
	replay := httptest.NewRecorder()
	h.ServeHTTP(replay, r)
	if replay.Code != 400 {
		t.Fatal("OAuth replay accepted")
	}
	link := httptest.NewRequest("POST", "/api/v1/auth/google/link", strings.NewReader("{}"))
	link.Header.Set("Origin", origin)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, link)
	if w.Code != 401 {
		t.Fatalf("unauthenticated linking %d", w.Code)
	}
}

func TestGoogleBrowserReturnIsBoundAndAllowlisted(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	origin, front := "https://api.example.invalid", "https://preview.example.invalid"
	h := New(db, Config{RegistrationEnabled: true, VerificationKey: make([]byte, 32), AppOrigin: origin, AllowedOrigins: []string{front}, CrossSiteCookies: true, GoogleProvider: &routeGoogleProvider{}})
	target := front + "/auth.html"
	for _, bad := range []string{"https://evil.example/auth.html", front + ".evil.example/auth.html", "https://user@preview.example.invalid/auth.html", front + "/auth.html#secret", front + "/other.html"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/auth/google/start?returnTo="+url.QueryEscape(bad)+"&redirect=1", nil))
		if w.Code != 422 {
			t.Fatalf("unsafe return accepted: %s %d", bad, w.Code)
		}
	}
	start := httptest.NewRecorder()
	h.ServeHTTP(start, httptest.NewRequest("GET", "/api/v1/auth/google/start?returnTo="+url.QueryEscape(target)+"&redirect=1", nil))
	if start.Code != 302 {
		t.Fatalf("browser start: %d %s", start.Code, start.Body.String())
	}
	authURL, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	cookie := start.Result().Cookies()[0]
	if cookie.SameSite != http.SameSiteNoneMode || !cookie.HttpOnly || !cookie.Secure {
		t.Fatal("unsafe cross-site binding cookie")
	}
	callback := "/api/v1/auth/google/callback?state=" + authURL.Query().Get("state") + "&code=code&returnTo=" + url.QueryEscape("https://evil.example/auth.html")
	r := httptest.NewRequest("GET", callback, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 302 || w.Header().Get("Location") != target || strings.Contains(w.Body.String(), "accessToken") {
		t.Fatalf("browser callback leaked tokens or redirected unsafely: %d %s", w.Code, w.Body.String())
	}
	var refresh *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == refreshCookieName {
			refresh = c
		}
	}
	if refresh == nil || !refresh.HttpOnly || !refresh.Secure {
		t.Fatal("no secure refresh session")
	}
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", nil)
	req.Header.Set("Origin", front)
	req.AddCookie(refresh)
	result := httptest.NewRecorder()
	h.ServeHTTP(result, req)
	if result.Code != 200 {
		t.Fatalf("frontend cannot finish sign-in: %d %s", result.Code, result.Body.String())
	}
	replay := httptest.NewRecorder()
	h.ServeHTTP(replay, r)
	if replay.Code != 400 {
		t.Fatal("browser flow replayed")
	}
}

func TestGoogleBrowserReturnErrorsStayBoundToTrustedPage(t *testing.T) {
	db := testpg.New(t)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO accounts(email,first_name,last_name,email_verified_at) VALUES('route-google@example.invalid','Existing','Member',now())`); err != nil {
		t.Fatal(err)
	}
	origin, front := "https://api.example.invalid", "https://preview.example.invalid"
	config := Config{RegistrationEnabled: true, VerificationKey: make([]byte, 32), AppOrigin: origin, AllowedOrigins: []string{front}, GoogleProvider: &routeGoogleProvider{}}
	h := New(db, config)
	target := front + "/City4Christ-PWA/auth.html"
	start := httptest.NewRecorder()
	h.ServeHTTP(start, httptest.NewRequest("GET", "/api/v1/auth/google/start?returnTo="+url.QueryEscape(target), nil))
	if start.Code != 200 {
		t.Fatalf("start %d %s", start.Code, start.Body.String())
	}
	var result struct{ Data struct{ URL string } }
	if err := json.Unmarshal(start.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	providerURL, err := url.Parse(result.Data.URL)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/auth/google/callback?state="+providerURL.Query().Get("state")+"&code=code", nil)
	request.AddCookie(start.Result().Cookies()[0])
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	if w.Code != 302 || w.Header().Get("Location") != target+"?auth_error=account_link_required" {
		t.Fatalf("unbound error redirect: %d %s", w.Code, w.Body.String())
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == refreshCookieName {
			t.Fatal("link-required created a session")
		}
	}
	var identities int
	if err := db.QueryRow(`SELECT count(*) FROM external_identities`).Scan(&identities); err != nil || identities != 0 {
		t.Fatal("implicitly linked an existing account")
	}
}
