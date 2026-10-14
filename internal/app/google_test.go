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
